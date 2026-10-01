// Package chart renders dependency-free candlestick PNGs for Telegram alerts.
package chart

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strconv"

	"github.com/mikekhromov/trade-orbit-bot/internal/core"
)

func Render(candles []core.Candle, targetText, currentText string) ([]byte, error) {
	if len(candles) == 0 {
		return nil, errors.New("chart has no candles")
	}
	if len(candles) > 80 {
		source := candles
		candles = make([]core.Candle, 0, 80)
		for index := 0; index < 80; index++ {
			candles = append(candles, source[index*(len(source)-1)/79])
		}
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 900, 420))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{10, 13, 17, 255}}, image.Point{}, draw.Src)
	grid := color.RGBA{34, 41, 51, 255}
	for _, y := range []int{40, 120, 200, 280, 360} {
		line(canvas, 40, y, 860, y, grid)
	}
	high, low := math.Inf(-1), math.Inf(1)
	for _, item := range candles {
		h, e1 := strconv.ParseFloat(item.High, 64)
		l, e2 := strconv.ParseFloat(item.Low, 64)
		if e1 != nil || e2 != nil {
			return nil, errors.New("invalid candle")
		}
		high, low = math.Max(high, h), math.Min(low, l)
	}
	target, targetErr := strconv.ParseFloat(targetText, 64)
	current, currentErr := strconv.ParseFloat(currentText, 64)
	if targetErr == nil {
		high, low = math.Max(high, target), math.Min(low, target)
	}
	if currentErr == nil {
		high, low = math.Max(high, current), math.Min(low, current)
	}
	span := high - low
	if span == 0 {
		span = 1
	}
	high += span * .06
	low -= span * .06
	span = high - low
	toY := func(value float64) int { return 30 + int((high-value)/span*330) }
	step := 820.0 / float64(len(candles))
	for index, item := range candles {
		o, e1 := strconv.ParseFloat(item.Open, 64)
		c, e2 := strconv.ParseFloat(item.Close, 64)
		h, e3 := strconv.ParseFloat(item.High, 64)
		l, e4 := strconv.ParseFloat(item.Low, 64)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			return nil, errors.New("invalid candle")
		}
		x := 40 + int((float64(index)+.5)*step)
		shade := color.RGBA{24, 184, 154, 255}
		if c < o {
			shade = color.RGBA{240, 68, 82, 255}
		}
		line(canvas, x, toY(h), x, toY(l), shade)
		width := max(2, int(step*.55))
		top, bottom := toY(math.Max(o, c)), toY(math.Min(o, c))
		if bottom <= top {
			bottom = top + 2
		}
		draw.Draw(canvas, image.Rect(x-width/2, top, x+width/2, bottom), &image.Uniform{C: shade}, image.Point{}, draw.Src)
	}
	if targetErr == nil {
		dashed(canvas, 40, toY(target), 860, color.RGBA{245, 182, 66, 255})
	}
	if currentErr == nil {
		line(canvas, 40, toY(current), 860, toY(current), color.RGBA{87, 154, 255, 255})
	}
	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func line(target *image.RGBA, x0, y0, x1, y1 int, shade color.Color) {
	if x0 == x1 {
		for y := min(y0, y1); y <= max(y0, y1); y++ {
			target.Set(x0, y, shade)
		}
		return
	}
	for x := min(x0, x1); x <= max(x0, x1); x++ {
		target.Set(x, y0, shade)
	}
}
func dashed(target *image.RGBA, x0, y, x1 int, shade color.Color) {
	for x := x0; x <= x1; x++ {
		if (x-x0)%12 < 7 {
			target.Set(x, y, shade)
		}
	}
}
