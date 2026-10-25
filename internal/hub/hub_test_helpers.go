//go:build testing

package hub

import (
	"github.com/henrygd/beszel/internal/hub/billing"
	"github.com/henrygd/beszel/internal/hub/systems"
)

// TESTING ONLY: GetSystemManager returns the system manager
func (h *Hub) GetSystemManager() *systems.SystemManager {
	return h.sm
}

// TESTING ONLY: GetBillingManager returns the billing manager
func (h *Hub) GetBillingManager() *billing.Manager {
	return h.bm
}

// TESTING ONLY: GetPubkey returns the public key
func (h *Hub) GetPubkey() string {
	return h.pubKey
}

// TESTING ONLY: SetPubkey sets the public key
func (h *Hub) SetPubkey(pubkey string) {
	h.pubKey = pubkey
}

func (h *Hub) SetCollectionAuthSettings() error {
	return setCollectionAuthSettings(h)
}
