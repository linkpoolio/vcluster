package nodes

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

const (
	// HostNodeHashLabel is set on virtual nodes to map them back to host nodes via SHA256 hash.
	HostNodeHashLabel = "vcluster.loft.sh/host-node-hash"
	// HostNodeNameAnnotation stores the real host node name for reverse lookups on restart.
	HostNodeNameAnnotation = "vcluster.loft.sh/host-node-name"
)

// nodeNameTranslator maps real host node names to opaque virtual names (the hash)
// and maintains a bidirectional lookup for nodeNeeded checks.
type nodeNameTranslator struct {
	mu            sync.RWMutex
	realToVirtual map[string]string
	virtualToReal map[string]string
}

func newNodeNameTranslator() *nodeNameTranslator {
	return &nodeNameTranslator{
		realToVirtual: make(map[string]string),
		virtualToReal: make(map[string]string),
	}
}

// Translate returns the virtual node name for a real hostname.
// The virtual name is the SHA256 hash prefix, which is opaque and deterministic.
func (t *nodeNameTranslator) Translate(realName string) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	if virtual, ok := t.realToVirtual[realName]; ok {
		return virtual
	}

	virtual := HostNodeHash(realName)
	t.realToVirtual[realName] = virtual
	t.virtualToReal[virtual] = realName
	return virtual
}

// RegisterExisting rebuilds the bidirectional mapping from an existing virtual node on restart.
func (t *nodeNameTranslator) RegisterExisting(realName, virtualName string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.realToVirtual[realName]; ok {
		return
	}

	t.realToVirtual[realName] = virtualName
	t.virtualToReal[virtualName] = realName
}

// VirtualToReal returns the real hostname for a virtual name.
func (t *nodeNameTranslator) VirtualToReal(virtualName string) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	real, ok := t.virtualToReal[virtualName]
	return real, ok
}

// RealToVirtual returns the virtual name for a real hostname without creating a mapping.
func (t *nodeNameTranslator) RealToVirtual(realName string) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	virtual, ok := t.realToVirtual[realName]
	return virtual, ok
}

// HostNodeHash computes a 12-character hex hash of a hostname for label storage.
func HostNodeHash(name string) string {
	h := sha256.Sum256([]byte(name))
	return hex.EncodeToString(h[:])[:12]
}
