package p2p

import (
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

func DeriveIdentityFromSeed(seed []byte) (crypto.PrivKey, peer.ID, error) {
	return fromSeed(seed)
}

func DerivePeerIDFromSeed(seed []byte) (peer.ID, error) {
	_, pid, err := fromSeed(seed)
	if err != nil {
		return "", err
	}
	return pid, nil
}

func DerivePeerIDFromPublicKey(pub []byte) (peer.ID, error) {
	return peerIDFromPublicKey(pub)
}
