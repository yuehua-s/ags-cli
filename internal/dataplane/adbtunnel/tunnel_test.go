package adbtunnel

import (
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func requireLocalListen() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		Skip("local listener unavailable: " + err.Error())
	}
	_ = ln.Close()
}

func newTestTunnel(endpoint string, tokenProvider func() (string, error)) *Tunnel {
	tunnel, err := New(TunnelOptions{
		InstanceID:    "sandbox-test",
		Domain:        "ap-guangzhou.tencentags.com",
		Endpoint:      endpoint,
		Insecure:      true,
		TokenProvider: tokenProvider,
		Logger:        log.New(io.Discard, "", 0),
	})
	Expect(err).NotTo(HaveOccurred())
	return tunnel
}

func runSession(tunnel *Tunnel, timeout string) {
	localConn, peerConn := net.Pipe()
	defer func() { _ = peerConn.Close() }()

	done := make(chan struct{})
	go func() {
		tunnel.handleConnectionWithReconnect(localConn)
		close(done)
	}()
	Eventually(done, timeout).Should(BeClosed())
}

var _ = Describe("ADB tunnel", func() {
	It("validates tunnel options", func() {
		_, err := New(TunnelOptions{InstanceID: "sandbox-test", Domain: "ap-guangzhou.tencentags.com", TokenProvider: func() (string, error) { return "token", nil }})
		Expect(err).NotTo(HaveOccurred())
		_, err = New(TunnelOptions{Domain: "ap-guangzhou.tencentags.com", TokenProvider: func() (string, error) { return "token", nil }})
		Expect(err).To(MatchError(ContainSubstring("instanceID")))
		_, err = New(TunnelOptions{InstanceID: "sandbox-test", TokenProvider: func() (string, error) { return "token", nil }})
		Expect(err).To(MatchError(ContainSubstring("domain")))
		_, err = New(TunnelOptions{InstanceID: "sandbox-test", Domain: "ap-guangzhou.tencentags.com"})
		Expect(err).To(MatchError(ContainSubstring("tokenProvider")))
	})

	It("constructs websocket URL and host", func() {
		tunnel, err := New(TunnelOptions{InstanceID: "sandbox-aaa", Domain: "ap-guangzhou.tencentags.com", TokenProvider: func() (string, error) { return "token", nil }})
		Expect(err).NotTo(HaveOccurred())
		Expect(tunnel.wsURL).To(Equal("wss://5556-sandbox-aaa.ap-guangzhou.tencentags.com/adb/ws"))
		Expect(tunnel.e2bHost).To(Equal("5556-sandbox-aaa.ap-guangzhou.tencentags.com"))
	})

	It("can reserve a local listener", func() { requireLocalListen() })

	It("tracks only immediate dial/token failures for degraded counting", func() {
		Expect(shouldTrackDialFailure(errors.New("WebSocket dial failed: bad handshake"), 200*time.Millisecond)).To(BeTrue())
		Expect(shouldTrackDialFailure(errors.New("token provider failed: timeout"), 200*time.Millisecond)).To(BeTrue())
		Expect(shouldTrackDialFailure(errors.New("ping write failed"), 200*time.Millisecond)).To(BeFalse())
		Expect(shouldTrackDialFailure(errors.New("WebSocket dial failed: bad handshake"), 2*time.Second)).To(BeFalse())
	})

	It("caps in-session reconnect backoff", func() {
		Expect(sessionReconnectDelay(1)).To(Equal(sessionReconnectBaseDelay))
		Expect(sessionReconnectDelay(2)).To(BeNumerically(">", sessionReconnectBaseDelay))
		Expect(sessionReconnectDelay(10)).To(Equal(sessionReconnectMaxDelay))
	})

	It("does not reconnect when server preempts with close code 4001", func() {
		var attempts atomic.Int32
		upgrader := websocket.Upgrader{}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			attempts.Add(1)
			go func() {
				defer conn.Close()
				_ = conn.WriteControl(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(closeCodePreempted, "preempted by newer client"),
					time.Now().Add(500*time.Millisecond),
				)
			}()
		}))
		defer server.Close()

		tunnel := newTestTunnel(strings.TrimPrefix(server.URL, "https://"), func() (string, error) {
			return "token", nil
		})

		runSession(tunnel, "3s")
		Expect(attempts.Load()).To(Equal(int32(1)))
	})

	It("retries up to max session reconnect attempts on non-preempt errors", func() {
		var attempts atomic.Int32
		upgrader := websocket.Upgrader{}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			attempts.Add(1)
			go func() {
				defer conn.Close()
				_ = conn.WriteControl(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, "server close"),
					time.Now().Add(500*time.Millisecond),
				)
			}()
		}))
		defer server.Close()

		tunnel := newTestTunnel(strings.TrimPrefix(server.URL, "https://"), func() (string, error) {
			return "token", nil
		})

		runSession(tunnel, "6s")
		Expect(attempts.Load()).To(Equal(int32(maxSessionReconnectAttempts + 1)))
	})

	It("enters degraded mode after dial failures accumulate across sessions", func() {
		var tokenCalls atomic.Int32
		tunnel := newTestTunnel("127.0.0.1:65535", func() (string, error) {
			tokenCalls.Add(1)
			return "", errors.New("mock token error")
		})
		defer tunnel.Stop()

		// First session consumes reconnect budget (initial + max reconnect attempts).
		runSession(tunnel, "5s")
		Expect(tunnel.State()).To(Equal(StateHealthy))
		Expect(tokenCalls.Load()).To(Equal(int32(maxSessionReconnectAttempts + 1)))

		// Next session's first immediate dial failure reaches maxDialFailures and degrades.
		runSession(tunnel, "2s")
		Eventually(func() TunnelState { return tunnel.State() }, "1s").Should(Equal(StateDegraded))
		Expect(tokenCalls.Load()).To(Equal(int32(maxSessionReconnectAttempts + 2)))
	})
})
