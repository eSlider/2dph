// Package contract — единый контракт записи leaf в Ladybug (2dph).
//
// Формализация по образцу gator contract.Record (G-8.0, #73): общий набор
// полей записи, обязательность и dedup-ключ, чтобы любой источник корпуса
// (mail, git, chats, docs, facts) писал единообразно. Пакет cgo-free:
// работает и тестируется без Ladybug.
//
// Связь с runtime-схемой Ladybug (internal/brain): контрактный ContentHash —
// это будущий dedup-ключ (см. P-9.3); текущий runtime id остаётся
// sha256(source\0text)[:24] и в этой задаче НЕ меняется.
package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
)

// Leaf — одна запись корпуса: контент + identity + темпоральность.
type Leaf struct {
	Source     string // корпус-источник: mail/git/chats/docs/facts
	ExternalID string // устойчивый ref внутри корпуса: message id / commit sha+path / chat message id
	Kind       string // тип leaf: fact/mail/chat/commit/reference/seed/...
	Text       string // содержимое leaf

	Root       string // facts|info
	Confidence string // confirmed|estimated|inferred|...
	SourceRev  string // ревизия источника (working-tree / sha)
	How        string // как получено: kb/index, mail/import, git-log, facts/extract
	Loc        string // локальный контекст (путь/репо)

	ValidFrom  string // D24-день начала валидности (YYYY-MM-DD), ортогонально версии
	ValidTo    string // D24-день конца валидности
	ObservedAt string // когда контент увиден (RFC3339); НЕ входит в ContentHash
}

// Validate отвергает leaf без identity/kind/контента — такие записи бесполезны
// и для dedup, и для схемы, поэтому режект на границе записи, до стора.
func (l Leaf) Validate() error {
	switch {
	case l.Source == "":
		return errors.New("contract: source is required")
	case l.ExternalID == "":
		return errors.New("contract: external_id is required")
	case l.Kind == "":
		return errors.New("contract: kind is required")
	case l.Text == "":
		return errors.New("contract: text is required")
	}
	return nil
}

// Source — источник корпуса (P-9.3), по образцу gator contract.Source
// (G-8.0 #73). Каждый корпус (mail/git/chats/docs) реализует Source и
// стримит leafs через emit; единый index-драйвер собирает их и пишет одним
// writer'ом. Пакет cgo-free — адаптеры работают и тестируются без Ladybug.
type Source interface {
	// Name — имя корпуса: mail / git / chats / docs. Используется как source
	// в Leaf и для registry-пересборки.
	Name() string
	// Stream вызывает emit для каждого leaf корпуса. Ошибка emit или
	// источника прерывает стрим. ctx отменяет стрим.
	Stream(ctx context.Context, emit func(Leaf) error) error
}

// NormalizeText — единая нормализация текста перед ContentHash/записью
// (P-9.3 #5.3): валидный UTF-8 (невалидный байт → U+FFFD), LF-переводы строк,
// без внешних пробелов/пустых строк. Один контент из разных путей (CRLF vs
// LF, хвостовые пробелы) даёт один hash.
func NormalizeText(text string) string {
	text = strings.ToValidUTF8(text, "\uFFFD")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.TrimSpace(text)
}

// ContentHash — dedup-ключ версии: sha256(source|external_id|kind|text)[:32].
//
// text нормализуется через NormalizeText перед хэшем (P-9.3 #5.3), поэтому
// id стабилен между источниками: один контент из разных путей даёт один ключ.
//
// observed_at намеренно вне хэша — семантика gator ContentHash (G-8.0 #73):
// тот же контент, записанный позже, дедуплицируется к той же версии
// (сохраняется первый observed_at); изменился контент → новый ключ и новая
// версия. Усечение до 32 hex-символов держит ключи короткими; коллизии
// пренебрежимы на масштабе корпуса.
func (l Leaf) ContentHash() string {
	h := sha256.New()
	io.WriteString(h, l.Source)
	io.WriteString(h, "|")
	io.WriteString(h, l.ExternalID)
	io.WriteString(h, "|")
	io.WriteString(h, l.Kind)
	io.WriteString(h, "|")
	io.WriteString(h, NormalizeText(l.Text))
	return hex.EncodeToString(h.Sum(nil))[:32]
}
