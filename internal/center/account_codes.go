package center

import (
	"encoding/hex"

	"github.com/shusfun/weir/internal/accountsecurity"
)

func (s *Server) emailCodes() accountsecurity.Codes {
	key, _ := hex.DecodeString(s.Config.EncryptionKey)
	return accountsecurity.Codes{Store: s.Store.KV, Locks: s.Store.Lock, Key: key, Policy: accountsecurity.DefaultCodePolicy()}
}
