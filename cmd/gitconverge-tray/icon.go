// 本文件生成 GitConverge 托盘图标，使用两条汇聚路径表达多仓库收敛。
//go:build windows

package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
)

func trayIcon() []byte {
	const size = 32
	canvas := image.NewRGBA(image.Rect(0, 0, size, size))
	ink := color.RGBA{R: 20, G: 33, B: 61, A: 255}
	blue := color.RGBA{R: 20, G: 99, B: 255, A: 255}
	cyan := color.RGBA{R: 4, G: 169, B: 199, A: 255}
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	for y := 2; y < 30; y++ {
		for x := 2; x < 30; x++ {
			if rounded(x, y, 2, 2, 28, 28, 7) {
				canvas.SetRGBA(x, y, ink)
			}
		}
	}
	drawLine(canvas, 8, 9, 16, 16, blue, 1)
	drawLine(canvas, 8, 23, 16, 16, cyan, 1)
	drawLine(canvas, 16, 16, 24, 16, white, 1)
	drawDot(canvas, 8, 9, blue)
	drawDot(canvas, 8, 23, cyan)
	drawDot(canvas, 24, 16, white)
	var pngBuffer bytes.Buffer
	_ = png.Encode(&pngBuffer, canvas)
	var icon bytes.Buffer
	_ = binary.Write(&icon, binary.LittleEndian, uint16(0))
	_ = binary.Write(&icon, binary.LittleEndian, uint16(1))
	_ = binary.Write(&icon, binary.LittleEndian, uint16(1))
	icon.WriteByte(size)
	icon.WriteByte(size)
	icon.WriteByte(0)
	icon.WriteByte(0)
	_ = binary.Write(&icon, binary.LittleEndian, uint16(1))
	_ = binary.Write(&icon, binary.LittleEndian, uint16(32))
	_ = binary.Write(&icon, binary.LittleEndian, uint32(pngBuffer.Len()))
	_ = binary.Write(&icon, binary.LittleEndian, uint32(22))
	_, _ = icon.Write(pngBuffer.Bytes())
	return icon.Bytes()
}

func rounded(x, y, left, top, width, height, radius int) bool {
	right, bottom := left+width-1, top+height-1
	if x < left || x > right || y < top || y > bottom {
		return false
	}
	centerX, centerY := x, y
	if x < left+radius {
		centerX = left + radius
	} else if x > right-radius {
		centerX = right - radius
	}
	if y < top+radius {
		centerY = top + radius
	} else if y > bottom-radius {
		centerY = bottom - radius
	}
	deltaX, deltaY := x-centerX, y-centerY
	return deltaX*deltaX+deltaY*deltaY <= radius*radius
}

func drawLine(canvas *image.RGBA, startX, startY, endX, endY int, value color.RGBA, thickness int) {
	deltaX, deltaY := abs(endX-startX), -abs(endY-startY)
	stepX, stepY := -1, -1
	if startX < endX {
		stepX = 1
	}
	if startY < endY {
		stepY = 1
	}
	err := deltaX + deltaY
	for {
		for offsetY := -thickness; offsetY <= thickness; offsetY++ {
			for offsetX := -thickness; offsetX <= thickness; offsetX++ {
				point := image.Pt(startX+offsetX, startY+offsetY)
				if point.In(canvas.Bounds()) {
					canvas.SetRGBA(point.X, point.Y, value)
				}
			}
		}
		if startX == endX && startY == endY {
			return
		}
		twice := 2 * err
		if twice >= deltaY {
			err += deltaY
			startX += stepX
		}
		if twice <= deltaX {
			err += deltaX
			startY += stepY
		}
	}
}

func drawDot(canvas *image.RGBA, centerX, centerY int, value color.RGBA) {
	for y := -2; y <= 2; y++ {
		for x := -2; x <= 2; x++ {
			if x*x+y*y <= 4 {
				canvas.SetRGBA(centerX+x, centerY+y, value)
			}
		}
	}
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
