package client

import (
	"errors"
	"fmt"
	"testing"

	"github.com/segmentio/kafka-go"
)

func TestIsTopicAlreadyExists(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil — не считается",
			err:  nil,
			want: false,
		},
		{
			name: "сырое kafka.TopicAlreadyExists",
			err:  kafka.TopicAlreadyExists,
			want: true,
		},
		{
			name: "обёрнуто через %w",
			err:  fmt.Errorf("create topic: %w", kafka.TopicAlreadyExists),
			want: true,
		},
		{
			name: "двойная обёртка через %w",
			err: fmt.Errorf("outer: %w",
				fmt.Errorf("inner: %w", kafka.TopicAlreadyExists),
			),
			want: true,
		},
		{
			name: "другая ошибка Kafka",
			err:  kafka.UnknownTopicOrPartition,
			want: false,
		},
		{
			name: "обычная ошибка",
			err:  errors.New("boom"),
			want: false,
		},
		{
			// Опасный кейс: %v не разворачивает ошибку,
			// errors.Is/As не смогут её распознать.
			name: "обёрнуто через %v — не разворачивается",
			err:  fmt.Errorf("create topic: %v", kafka.TopicAlreadyExists),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTopicAlreadyExists(tt.err)
			if got != tt.want {
				t.Fatalf("isTopicAlreadyExists(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
