package domain

import "context"

type ClickRequest struct {
	UserID   int64 `json:"user_id"`
	AuthorID int64 `json:"author_id"`
}

type Config struct {
	TargetURL            string `json:"target_url"`
	AuthorIDStart        int64  `json:"author_id_start"`
	AuthorIDEnd          int64  `json:"author_id_end"`
	ReaderIDStart        int64  `json:"reader_id_start"`
	ReaderIDEnd          int64  `json:"reader_id_end"`
	MinReads             int    `json:"min_reads"`
	MaxReads             int    `json:"max_reads"`
	DelayBetweenReadsSec int    `json:"delay_between_reads_sec"`
}

// ClickSender контракт на отправку клика (порт)
type ClickSender interface {
	Send(ctx context.Context, req ClickRequest) error
}
