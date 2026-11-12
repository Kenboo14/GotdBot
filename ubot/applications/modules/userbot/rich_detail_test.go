package userbot

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram/message/rich"
	"github.com/gotd/td/tg"
)

func TestRichMessageDetailBuilder(t *testing.T) {
	itemNum := 1
	htmlContent := "<p><b>Promo Spesial!</b> Diskon hingga 50% untuk semua member.</p>"

	blocks, err := rich.ParseHTML(bytes.NewBufferString(htmlContent))
	if err != nil || len(blocks) == 0 {
		blocks = []tg.PageBlockClass{rich.Paragraph(rich.Plain(htmlContent))}
	}

	allBlocks := []tg.PageBlockClass{
		rich.Heading1(rich.Plain(fmt.Sprintf("Detail List #%d", itemNum))),
		rich.Divider(),
	}
	allBlocks = append(allBlocks, blocks...)

	// Mock photos and documents
	photos := []tg.InputPhotoClass{
		&tg.InputPhoto{ID: 1001, AccessHash: 2002, FileReference: []byte("ref1")},
	}
	allBlocks = append(allBlocks, rich.Photo(1001, rich.Caption(rich.Plain("Foto Produk"), rich.Empty())))

	documents := []tg.InputDocumentClass{
		&tg.InputDocument{ID: 3003, AccessHash: 4004, FileReference: []byte("ref2")},
	}
	allBlocks = append(allBlocks, rich.Video(3003, rich.Caption(rich.Plain("Video Demo"), rich.Empty())))

	msg := rich.New(allBlocks...).Photos(photos...).Documents(documents...)
	input := msg.Input()

	if len(input.Blocks) != 5 {
		t.Fatalf("expected 5 blocks (heading, divider, paragraph, photo, video), got %d", len(input.Blocks))
	}

	heading, ok := input.Blocks[0].(*tg.PageBlockHeading1)
	if !ok {
		t.Fatalf("expected first block to be PageBlockHeading1, got %T", input.Blocks[0])
	}
	if heading.Text == nil {
		t.Fatal("expected heading text to be non-nil")
	}

	if _, ok := input.Blocks[1].(*tg.PageBlockDivider); !ok {
		t.Fatalf("expected second block to be PageBlockDivider, got %T", input.Blocks[1])
	}

	if len(input.Photos) != 1 || input.Photos[0].(*tg.InputPhoto).ID != 1001 {
		t.Fatalf("expected 1 photo with ID 1001, got %+v", input.Photos)
	}

	if len(input.Documents) != 1 || input.Documents[0].(*tg.InputDocument).ID != 3003 {
		t.Fatalf("expected 1 document with ID 3003, got %+v", input.Documents)
	}

	// Verify binary encoding
	b := &bin.Buffer{}
	if err := input.Encode(b); err != nil {
		t.Fatalf("failed to encode InputRichMessage: %v", err)
	}
	if b.Len() == 0 {
		t.Fatal("encoded buffer should not be empty")
	}
}
