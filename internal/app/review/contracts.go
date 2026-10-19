// Package review owns review-feedback validation, trusted context projection,
// and atomic cross-ledger consumption for writing and project Agent chats.
package reviewapp

import "denova/internal/book"

// Runtime is an immutable Project snapshot shared by Book and General conversations.
type Runtime struct {
	Workspace   string
	StateRoot   string
	SessionID   string
	BookService *book.Service
}
