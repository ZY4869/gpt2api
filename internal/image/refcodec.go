package image

import (
	"net/url"
	"strconv"
	"strings"
)

const storedRefPrefix = "meta:"

type StoredRef struct {
	AccountID      uint64
	ConversationID string
	Ref            string
}

func EncodeStoredRef(accountID uint64, conversationID, ref string) string {
	if strings.TrimSpace(ref) == "" {
		return ""
	}
	values := url.Values{}
	values.Set("ref", ref)
	if accountID > 0 {
		values.Set("account_id", strconv.FormatUint(accountID, 10))
	}
	if strings.TrimSpace(conversationID) != "" {
		values.Set("conversation_id", conversationID)
	}
	return storedRefPrefix + values.Encode()
}

func DecodeStoredRef(raw string) StoredRef {
	ref := StoredRef{Ref: raw}
	if !strings.HasPrefix(raw, storedRefPrefix) {
		return ref
	}
	values, err := url.ParseQuery(strings.TrimPrefix(raw, storedRefPrefix))
	if err != nil {
		return ref
	}
	if parsed := strings.TrimSpace(values.Get("ref")); parsed != "" {
		ref.Ref = parsed
	}
	ref.ConversationID = strings.TrimSpace(values.Get("conversation_id"))
	if accountID, err := strconv.ParseUint(strings.TrimSpace(values.Get("account_id")), 10, 64); err == nil {
		ref.AccountID = accountID
	}
	return ref
}

func PublicFileID(raw string) string {
	return strings.TrimPrefix(DecodeStoredRef(raw).Ref, "sed:")
}

func ResolveStoredRef(raw string, fallbackAccountID uint64, fallbackConversationID string) StoredRef {
	ref := DecodeStoredRef(raw)
	if ref.AccountID == 0 {
		ref.AccountID = fallbackAccountID
	}
	if ref.ConversationID == "" {
		ref.ConversationID = fallbackConversationID
	}
	return ref
}
