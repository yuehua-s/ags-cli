package token

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("token cache", func() {
	It("rejects symlink cache files", func() {
		dir := GinkgoT().TempDir()
		target := filepath.Join(dir, "real.json")
		Expect(os.WriteFile(target, []byte(`{"version":1,"tokens":{}}`), 0o600)).To(Succeed())
		link := filepath.Join(dir, "tokens.json")
		Expect(os.Symlink(target, link)).To(Succeed())
		cache := &Cache{path: link, lockPath: link + ".lock"}
		_, err := cache.loadLocked()
		Expect(err).To(HaveOccurred())
	})

	It("quarantines corrupted cache files", func() {
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, "tokens.json")
		Expect(os.WriteFile(path, []byte("{bad json"), 0o600)).To(Succeed())
		cache := &Cache{path: path, lockPath: path + ".lock"}
		_, err := cache.loadLocked()
		Expect(err).To(HaveOccurred())
		matches, err := filepath.Glob(path + ".corrupt-*")
		Expect(err).NotTo(HaveOccurred())
		Expect(matches).To(HaveLen(1))
	})

	It("returns miss for explicitly expired token", func() {
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, "tokens.json")
		cache := &Cache{path: path, lockPath: path + ".lock"}

		err := cache.SetWithExpiry("inst-explicit", "token-explicit", time.Now().Add(-1*time.Minute))
		Expect(err).NotTo(HaveOccurred())

		token, found := cache.Get("inst-explicit")
		Expect(found).To(BeFalse())
		Expect(token).To(BeEmpty())
	})

	It("returns miss for legacy token older than default ttl", func() {
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, "tokens.json")
		cache := &Cache{path: path, lockPath: path + ".lock"}

		legacy := &CacheData{
			Version: CacheVersion,
			Tokens: map[string]*TokenEntry{
				"inst-legacy": {
					AccessToken: "legacy-token",
					CreatedAt:   time.Now().Add(-(defaultTokenTTL + time.Hour)),
				},
			},
		}

		payload, err := json.Marshal(legacy)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(path, payload, 0o600)).To(Succeed())

		token, found := cache.Get("inst-legacy")
		Expect(found).To(BeFalse())
		Expect(token).To(BeEmpty())
	})

	It("treats zero timestamps token as expired and cleans it", func() {
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, "tokens.json")
		cache := &Cache{path: path, lockPath: path + ".lock"}

		legacy := &CacheData{
			Version: CacheVersion,
			Tokens: map[string]*TokenEntry{
				"inst-zero": {
					AccessToken: "legacy-zero-token",
					CreatedAt:   time.Time{},
					ExpiresAt:   time.Time{},
				},
			},
		}

		payload, err := json.Marshal(legacy)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(path, payload, 0o600)).To(Succeed())

		token, found := cache.Get("inst-zero")
		Expect(found).To(BeFalse())
		Expect(token).To(BeEmpty())

		raw, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		var after CacheData
		Expect(json.Unmarshal(raw, &after)).To(Succeed())
		_, exists := after.Tokens["inst-zero"]
		Expect(exists).To(BeFalse())
	})
})
