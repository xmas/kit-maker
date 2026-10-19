// Package secrets seals integration configs and settings before they are
// stored, using the deployment's keeper (DESIGN.md §1.2, §11.1):
//
//	base64key://<32 random bytes, URL-safe base64>   local key (compose, sbx, small installs)
//	awskms://<key id>?region=…                       AWS KMS
//	azurekeyvault://<vault>.vault.azure.net/keys/<key>
//	gcpkms://projects/…/cryptoKeys/<key>
//
// The URL comes from KITMAKER_SECRETS_URL.
package secrets

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"gocloud.dev/secrets"
	_ "gocloud.dev/secrets/awskms"
	_ "gocloud.dev/secrets/azurekeyvault"
	_ "gocloud.dev/secrets/gcpkms"
	_ "gocloud.dev/secrets/localsecrets"
)

// Keeper seals and opens values.
type Keeper struct {
	k *secrets.Keeper
}

// ErrNotConfigured means KITMAKER_SECRETS_URL is empty.
var ErrNotConfigured = errors.New("missing required env: KITMAKER_SECRETS_URL (e.g. base64key://… from `kitmaker secrets new-key`)")

// Open opens the keeper at url.
func Open(ctx context.Context, url string) (*Keeper, error) {
	if url == "" {
		return nil, ErrNotConfigured
	}
	k, err := secrets.OpenKeeper(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open secrets keeper: %w", err)
	}
	return &Keeper{k: k}, nil
}

// NewLocalKeyURL returns a fresh base64key:// URL for a local keeper.
func NewLocalKeyURL() (string, error) {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return "", err
	}
	return "base64key://" + base64.URLEncoding.EncodeToString(key[:]), nil
}

// SealJSON marshals v and encrypts it.
func (k *Keeper) SealJSON(ctx context.Context, v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return k.k.Encrypt(ctx, b)
}

// OpenJSON decrypts sealed and unmarshals it into v.
func (k *Keeper) OpenJSON(ctx context.Context, sealed []byte, v any) error {
	b, err := k.k.Decrypt(ctx, sealed)
	if err != nil {
		return fmt.Errorf("open sealed value: %w", err)
	}
	return json.Unmarshal(b, v)
}

func (k *Keeper) Close() error { return k.k.Close() }
