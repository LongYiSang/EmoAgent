package plugin

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	SignatureStatusVerified         = "verified"
	SignatureStatusUnsignedDev      = "unsigned_dev"
	SignatureStatusMissingSignature = "missing_signature"
	SignatureStatusBadSignature     = "bad_signature"
	SignatureStatusUnknownPublisher = "unknown_publisher"
	SignatureStatusDigestMismatch   = "digest_mismatch"
)

type TrustedPublishers struct {
	Publishers []TrustedPublisher `yaml:"publishers" json:"publishers"`
}

type TrustedPublisher struct {
	ID          string             `yaml:"id" json:"id"`
	DisplayName string             `yaml:"display_name" json:"display_name"`
	PublicKeys  []TrustedPublicKey `yaml:"public_keys" json:"public_keys"`
}

type TrustedPublicKey struct {
	ID              string `yaml:"id" json:"id"`
	Algorithm       string `yaml:"algorithm" json:"algorithm"`
	PublicKeyBase64 string `yaml:"public_key_base64" json:"public_key_base64"`
}

type PluginReleaseDescriptor struct {
	PluginID        string `yaml:"plugin_id" json:"plugin_id"`
	Version         string `yaml:"version" json:"version"`
	PackageDigest   string `yaml:"package_digest" json:"package_digest"`
	ManifestDigest  string `yaml:"manifest_digest" json:"manifest_digest"`
	PublisherID     string `yaml:"publisher_id" json:"publisher_id"`
	KeyID           string `yaml:"key_id" json:"key_id"`
	SignatureBase64 string `yaml:"signature_base64" json:"signature_base64"`
}

func LoadTrustedPublishers(path string) (TrustedPublishers, error) {
	if strings.TrimSpace(path) == "" {
		return TrustedPublishers{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return TrustedPublishers{}, err
	}
	var publishers TrustedPublishers
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&publishers); err != nil {
		return TrustedPublishers{}, err
	}
	return publishers, nil
}

func DecodeReleaseDescriptor(data []byte) (PluginReleaseDescriptor, error) {
	var descriptor PluginReleaseDescriptor
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&descriptor); err != nil {
		return PluginReleaseDescriptor{}, err
	}
	return descriptor, nil
}

func VerifyReleaseDescriptor(descriptor PluginReleaseDescriptor, publishers TrustedPublishers, pluginID, version, packageDigest, manifestDigest string) (string, error) {
	if strings.TrimSpace(descriptor.PluginID) == "" {
		return SignatureStatusMissingSignature, fmt.Errorf("release descriptor plugin_id is required")
	}
	if descriptor.PluginID != pluginID || descriptor.Version != version {
		return SignatureStatusDigestMismatch, fmt.Errorf("release descriptor targets %s@%s, got %s@%s", descriptor.PluginID, descriptor.Version, pluginID, version)
	}
	if descriptor.ManifestDigest != manifestDigest {
		return SignatureStatusDigestMismatch, fmt.Errorf("manifest digest mismatch")
	}
	if descriptor.PackageDigest != "" && packageDigest != "" && descriptor.PackageDigest != packageDigest {
		return SignatureStatusDigestMismatch, fmt.Errorf("package digest mismatch")
	}
	publisher, key, ok := findTrustedKey(publishers, descriptor.PublisherID, descriptor.KeyID)
	if !ok {
		return SignatureStatusUnknownPublisher, fmt.Errorf("publisher/key %s/%s is not trusted", descriptor.PublisherID, descriptor.KeyID)
	}
	if !strings.EqualFold(key.Algorithm, "ed25519") {
		return SignatureStatusUnknownPublisher, fmt.Errorf("unsupported key algorithm %q", key.Algorithm)
	}
	publicKey, err := base64.StdEncoding.DecodeString(key.PublicKeyBase64)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return SignatureStatusUnknownPublisher, fmt.Errorf("invalid public key for publisher %s", publisher.ID)
	}
	signature, err := base64.StdEncoding.DecodeString(descriptor.SignatureBase64)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return SignatureStatusBadSignature, fmt.Errorf("invalid signature encoding")
	}
	payload, err := descriptor.CanonicalPayload()
	if err != nil {
		return SignatureStatusBadSignature, err
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), payload, signature) {
		return SignatureStatusBadSignature, fmt.Errorf("signature verification failed")
	}
	return SignatureStatusVerified, nil
}

func (d PluginReleaseDescriptor) CanonicalPayload() ([]byte, error) {
	payload := struct {
		PluginID       string `json:"plugin_id"`
		Version        string `json:"version"`
		PackageDigest  string `json:"package_digest"`
		ManifestDigest string `json:"manifest_digest"`
		PublisherID    string `json:"publisher_id"`
		KeyID          string `json:"key_id"`
	}{
		PluginID:       d.PluginID,
		Version:        d.Version,
		PackageDigest:  d.PackageDigest,
		ManifestDigest: d.ManifestDigest,
		PublisherID:    d.PublisherID,
		KeyID:          d.KeyID,
	}
	return json.Marshal(payload)
}

func findTrustedKey(publishers TrustedPublishers, publisherID, keyID string) (TrustedPublisher, TrustedPublicKey, bool) {
	for _, publisher := range publishers.Publishers {
		if publisher.ID != publisherID {
			continue
		}
		for _, key := range publisher.PublicKeys {
			if key.ID == keyID {
				return publisher, key, true
			}
		}
	}
	return TrustedPublisher{}, TrustedPublicKey{}, false
}

func sha256Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
