package channels

import "context"

// Channel abstracts a messaging transport (HTTP, WebSocket, Feishu, etc.).
type Channel interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
