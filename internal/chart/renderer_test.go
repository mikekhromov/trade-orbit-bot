package chart

import (
	"bytes"
	"testing"

	"github.com/mikekhromov/trade-orbit-bot/internal/core"
)

func TestRenderProducesPNG(t *testing.T) {
	picture, err := Render([]core.Candle{
		{Open: "100", High: "105", Low: "98", Close: "103"},
		{Open: "103", High: "104", Low: "96", Close: "99"},
	}, "104", "103")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(picture, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatal("renderer did not produce PNG")
	}
}
