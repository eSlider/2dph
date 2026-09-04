// Package corpus — адаптеры источников корпуса (P-9.3), cgo-free.
//
// Каждый корпус (mail / git / chats / docs) реализует contract.Source:
//
//	Name() string                          // "mail" | "git" | "chats" | "docs"
//	Stream(ctx, emit func(contract.Leaf))  // стримит leafs, валидные по контракту
//
// Единый index-драйвер (bin/brain/index.go) собирает leafs через StreamAll и
// пишет одним writer'ом (brain.WriteCorpus). Добавление/удаление корпуса =
// добавление/удаление адаптера в registry + пересборка (детерминированные id
// = contract.ContentHash, поэтому пересборка идемпотентна).
package corpus

import (
	"context"
	"fmt"

	"github.com/eSlider/2dph/internal/contract"
)

// StreamAll прогоняет источники в порядке списка и вызывает emit для каждого
// leaf. Ошибка источника или emit прерывает стрим; имя корпуса попадает в
// сообщение ошибки для диагностики. Отменённый ctx останавливает стрим даже
// если источник игнорирует ctx (проверка между источниками).
func StreamAll(ctx context.Context, sources []contract.Source, emit func(contract.Leaf) error) error {
	for _, s := range sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.Stream(ctx, emit); err != nil {
			return fmt.Errorf("corpus %s: %w", s.Name(), err)
		}
	}
	return ctx.Err()
}
