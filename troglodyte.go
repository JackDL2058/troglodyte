// Copyright (c) 2026 Jack Durnin. All rights reserved.
// Use of this source code is governed by an MIT-style license.

package troglodyte

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/term"
)

var (
	BuildNumber    = "0.0.17" // Build version
	GlobalFixRatio = 2.0      // the ratio for fixed ratio shapes to use, which the x is multiplied by to result in a wider circle, to counter tall terminal characters.
	out            = bufio.NewWriterSize(os.Stdout, 1024*128)

	// Buffers for differential rendering
	backBuffer   [][]Pixel
	frontBuffer  [][]Pixel
	termW, termH int

	// Global Registry
	allSprites []*Sprite
	spriteMu   sync.RWMutex
	keyHandled = make(map[string]bool) // flags to prevent continuous key adding
)

type projection int

const (
	orthogonal projection = iota
	perspective
	isometric
)

// #region Constants & ANSI
const (
	// 19,19,19 (#131313)
	Black = "\033[30m"

	// 196,36,48 (#C42430)
	Red = "\033[31m"

	// 51,152,75 (#33984B)
	Green = "\033[32m"

	// 255,235,87 (#FFEB57)
	Yellow = "\033[33m"

	// 48,3,217 (#3003D9)
	Blue = "\033[34m"

	// 219,63,253 (#DB3FFD)
	Magenta = "\033[35m"

	// 15,155,155 (#0F9B9B)
	Cyan = "\033[36m"

	// 255,255,255 (#FFFFFF)
	White = "\033[37m"

	// Alpha: less than 255
	Default = "\033[39m"

	BgBlack   = "\033[40m"
	BgRed     = "\033[41m"
	BgGreen   = "\033[42m"
	BgYellow  = "\033[43m"
	BgBlue    = "\033[44m"
	BgMagenta = "\033[45m"
	BgCyan    = "\033[46m"
	BgWhite   = "\033[47m"
	BgDefault = "\033[49m"

	CursorHome = "\033[H"
	HideCursor = "\033[?25l"
	ShowCursor = "\033[?25h"
)

// #endregion

// #region Core Types

type Pixel struct {
	Char     string
	FgColour string
	BgColour string
}

// A sprite is a basic object in troglodyte. It can move, have pixel data, and will soon have support for animated textures.
// You also don't have to make the sprite data yourself! Import github.com/JackDL2058/troglodyte/troglosprite to download
// troglosprite, the newest tool for use along side troglodyte. It can take a png image and turn it into troglodyte pixel
// data, meaning you can simply draw your sprite in something like aseprite and convert to pixel data. There will also be an
// editor to make sprites and export the sprite data in future. Disclaimer: troglosprite can only use specific colours and will
// not convert any colour to pixel data. You can download the colour palette from lospec.
type Sprite struct {
	X, Y         float64 // Current position (float for smooth delta-time)
	Width        int     // Calculated from pixel data
	Height       int     // Calculated from pixel data
	Pixels       [][]Pixel
	Tags         []string
	Visible      bool
	Parent       *Sprite
	Children     []*Sprite
	mu           sync.RWMutex // This PROTECTS the X and Y values
	PrevX, PrevY float64      // The previous x and y position, useful for stuff like snake or anything that follows something else.
}

// A wall object in fake 3D. only works if troglodyte.Fake3DInit() has been called. Has nothing to do with True3D.
// This is essentially a line with 2 positions for either end. There might be an editor or something like that in future to make these easier to manage.
// To create one, use the NewFake3dWall function.
type Fake3dWall struct {
	X1             float64
	X2             float64
	Y1             float64
	Y2             float64
	Colour         string   // The colour code of the wall
	FallbackSymbol string   // the symbol to use if angle dither fails. If angle dither is off, this is used as the symbol.
	AngleDither    bool     // whether to dither based on the angle of a wall relative to the camera. If false, uses FallbackSymbol.
	Tags           []string // Slice of tags, to differentiate walls, maybe for different levels, which will be shown in an example.
}

// A raycast object in fake 3D. Used to determine the distance to a Fake3DWall or other Fake3D objects.
type Fake3DRaycast struct {
	Direction float64 // radians
	X         float64 // x position
	Y         float64 // y position
	Travelx   float64 // how far to move on the x axis, calculated by cos(direction)
	Travely   float64 // how far to move on the y axis, calculated by sin(direction)
	Distance  float64 // how many total units the raycast object has travelled.
}

// A box that you can enter text into. It needs to be focused on by using one of the keys in the FocusKeys field.
// To use mouse buttons in the FocusKeys field, use "lc" as left click, "rc" for right and "mc" for middle. Pressing enter will call the
// EnterFunction function.
type TextInput struct {
	X             int      // x position of the input box
	Y             int      // y position of the input box
	Text          string   // The actual text being entered into the box
	Prompt        string   // The prompt that appears in the box if no text has been entered
	FocusKeys     []string // A slice of keys required to focus or unfocus on the text input
	EnterFunction func()   // the function called when the enter key is pressed
}

func NewTextInput(x, y int, prompt string, focusKeys []string, enterFunction func()) *TextInput {
	return &TextInput{
		X:             x,
		Y:             y,
		Text:          "",
		Prompt:        prompt,
		FocusKeys:     focusKeys,
		EnterFunction: enterFunction,
	}
}

// #endregion

// #region Buffer Management

// initBuffers initializes the grids based on current terminal size.
func initBuffers() {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		w, h = 80, 24 // Fallback
	}
	termW, termH = w, h

	backBuffer = make([][]Pixel, termH)
	frontBuffer = make([][]Pixel, termH)
	for i := range backBuffer {
		backBuffer[i] = make([]Pixel, termW)
		frontBuffer[i] = make([]Pixel, termW)
		for j := 0; j < termW; j++ {
			empty := Pixel{Char: " ", FgColour: Default, BgColour: BgDefault}
			backBuffer[i][j] = empty
			frontBuffer[i][j] = empty
		}
	}
}

// SetPixel is the internal engine function to write to the backbuffer.
func SetPixel(x, y int, p Pixel) {
	// Terminal coordinates are 1-based for users, but 0-based for our buffer logic.
	// We adjust here so the user can think in 1-based terminal coords.
	tx, ty := x-1, y-1
	if ty >= 0 && ty < termH && tx >= 0 && tx < termW {
		backBuffer[ty][tx] = p
	}
}

// #endregion

// #region fake 3D logic

func NewFake3dWall(x1, x2, y1, y2 float64, colour, fallbackSymbol string, angleDither bool, optionalTags ...string) *Fake3dWall {
	tags := []string{}
	if len(optionalTags) > 0 {
		for i := range len(optionalTags) {
			tags = append(tags, optionalTags[i])
		}
	}
	return &Fake3dWall{x1, x2, y1, y2, colour, fallbackSymbol, angleDither, []string{}}
}

// #endregion

// #region Sprite Logic

func NewSprite(x, y int, pixels [][]Pixel) *Sprite {
	h := len(pixels)
	w := 0
	if h > 0 {
		w = len(pixels[0])
	}

	s := &Sprite{
		X: float64(x), Y: float64(y), Width: w, Height: h,
		Pixels: pixels, Visible: true, Tags: []string{},
	}

	spriteMu.Lock()
	allSprites = append(allSprites, s)
	spriteMu.Unlock()
	return s
}

func (s *Sprite) AddChild(child *Sprite) {
	s.mu.Lock()
	defer s.mu.Unlock()
	child.Parent = s
	s.Children = append(s.Children, child)
}

var lastFrameTime = time.Now()

func GetDeltaTime() float64 {
	now := time.Now()
	dt := now.Sub(lastFrameTime).Seconds()
	lastFrameTime = now
	return dt
}

// Moves a sprite by a certain distance, adding dx and dy to the sprite's position, and does the same to children if moveChildren is true.
func (s *Sprite) Move(dx, dy float64, moveChildren bool) {
	s.mu.Lock()

	s.PrevX = s.X
	s.PrevY = s.Y

	s.X += dx
	s.Y += dy

	s.mu.Unlock() // CRITICAL: Unlock the parent BEFORE moving children

	if moveChildren {
		for _, child := range s.Children {
			// This call will create its own independent lock
			child.Move(dx, dy, true)
		}
	}
}

// Moves a sprite directly to a target position, like moving directly to the mouse's position. Prevents having to
// calculate the relative x and y to a target position as you would when using Move(). Does the same to children if moveChildren is true.
func (s *Sprite) MoveDirect(x, y float64, moveChildren bool) {
	s.mu.Lock()

	s.PrevX = s.X
	s.PrevY = s.Y

	s.X = x
	s.Y = y
	s.mu.Unlock() // CRITICAL: Unlock the parent BEFORE moving children

	if moveChildren {
		for _, child := range s.Children {
			// This call will create its own independent lock
			child.MoveDirect(x, y, true)
		}
	}
}

// Removes a sprite from the scene, as well as all of its children. This simply stops tracking it and its children, so the sprite can be added back by using
// AddSpriteBack().
func (s *Sprite) Destroy() {
	spriteMu.Lock()
	defer spriteMu.RUnlock() // Note: Use Lock/Unlock for writing to the slice

	for i, sprite := range allSprites {
		if sprite == s {
			// Remove from the global registry
			allSprites = append(allSprites[:i], allSprites[i+1:]...)
			break
		}
	}

	// If it has a parent, remove it from the parent's children list too
	if s.Parent != nil {
		s.Parent.mu.Lock()
		for i, child := range s.Parent.Children {
			if child == s {
				s.Parent.Children = append(s.Parent.Children[:i], s.Parent.Children[i+1:]...)
				break
			}
		}
		s.Parent.mu.Unlock()
	}
}

// Adds a sprite back to the game after it's been deleted by the Destroy() method by adding it to allSprites and restoring its visibility.
func (s *Sprite) AddSpriteBack() {
	spriteMu.Lock()
	defer spriteMu.Unlock()

	// Add back to the global registry if not already present
	found := slices.Contains(allSprites, s)
	if !found {
		allSprites = append(allSprites, s)
	}

	// If it has a parent, add it back to the parent's children list if not already there
	if s.Parent != nil {
		s.Parent.mu.Lock()
		found = slices.Contains(s.Parent.Children, s)
		if !found {
			s.Parent.Children = append(s.Parent.Children, s)
		}
		s.Parent.mu.Unlock()
	}

	// Make sure it's visible
	s.Visible = true
}

func (s *Sprite) AddTag(tag string) { s.Tags = append(s.Tags, tag) }

func (s *Sprite) HasTag(tag string) bool { return slices.Contains(s.Tags, tag) }

func (s *Sprite) Draw() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.Visible || s.Pixels == nil {
		return
	}

	// Crucial: Cast to int AFTER the math to prevent jitter
	// We use floor math to ensure the sprite stays on the integer grid correctly
	offsetX := int(math.Round(s.X)) - (s.Width / 2)
	offsetY := int(math.Round(s.Y)) - (s.Height / 2)

	for y, row := range s.Pixels {
		for x, px := range row {
			// SetPixel handles the bounds checking internally
			SetPixel(offsetX+x, offsetY+y, px)
		}
	}
}

func DrawAllSprites() {
	spriteMu.RLock()
	// Create a local copy of pointers so we don't hold the Registry lock for long
	tempSprites := make([]*Sprite, len(allSprites))
	copy(tempSprites, allSprites)
	spriteMu.RUnlock()

	for _, s := range tempSprites {
		s.Draw()
	}
}

func DrawSpritesWithTag(tag string) {
	spriteMu.RLock()
	defer spriteMu.RUnlock()
	for _, s := range allSprites {
		if s.HasTag(tag) {
			s.Draw()
		}
	}
}

// #endregion

// #region Graphics

// drawTitle draws a text title using pixel data from letters.json at the specified position.
//
// x, y: starting position of the title
//
// title: the text to draw
//
// fg: foreground color (e.g., troglodyte.White, troglodyte.Cyan)
//
// bg: background color (e.g., troglodyte.BgDefault, "")
func DrawTitle(x, y int, title, fg, bg string) {

	// Load and parse letters.json
	data, err := os.ReadFile("letters.json")
	if err != nil {
		return
	}

	var letters map[string]map[string]string
	err = json.Unmarshal(data, &letters)
	if err != nil {
		return
	}

	title = strings.ToUpper(title)

	currentX := x
	// Draw each character in the title
	for _, char := range title {
		charStr := string(char)
		letterData, exists := letters[charStr]
		if !exists {
			// Skip characters not in the font
			continue
		}

		// Extract the 5 rows of pixel data
		rows := make([]string, 5)
		for i := 1; i <= 5; i++ {
			rowKey := string(rune('0' + i))
			if row, ok := letterData[rowKey]; ok {
				rows[i-1] = row
			}
		}

		// Draw the character's pixels
		for rowIdx, row := range rows {
			for colIdx, pixelChar := range row {
				if pixelChar == '#' {
					// Draw a filled pixel
					p := Pixel{Char: "█", FgColour: fg, BgColour: bg}
					SetPixel(currentX+colIdx, y+rowIdx, p)
				}
			}
		}

		// Move to the next character position
		if len(rows) > 0 {
			currentX += len(rows[0]) + 1 // +1 for spacing between characters
		}
	}
}

func DrawLine(x1, y1, x2, y2 int, char, fg, bg string) {
	dx := int(math.Abs(float64(x2 - x1)))
	dy := -int(math.Abs(float64(y2 - y1)))
	sx, sy := -1, -1
	if x1 < x2 {
		sx = 1
	}
	if y1 < y2 {
		sy = 1
	}
	err := dx + dy
	p := Pixel{char, fg, bg}

	for {
		SetPixel(x1, y1, p)
		if x1 == x2 && y1 == y2 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x1 += sx
		}
		if e2 <= dx {
			err += dx
			y1 += sy
		}
	}
}

func DrawRect(x, y, w, h int, char, fg, bg string) {
	p := Pixel{char, fg, bg}
	for i := range h {
		for j := range w {
			SetPixel(x+j, y+i, p)
		}
	}
}

// Project3D converts 3D coordinates (x, y, z) into 2D terminal coordinates (column, row).
// posX, posY and posZ is the position of the camera. Proj takes the orthogonal, perspective or isometric projection.
func Project3D(x, y, z float64, proj projection, posX, posY, posZ float64, fov float64) (int, int) {
	var screenX, screenY float64

	// Translate coordinates relative to the "camera" position
	relX := x - posX
	relY := y - posY
	relZ := z - posZ

	switch proj {
	case orthogonal:
		// Simple parallel projection, Z is ignored for position
		screenX = relX
		screenY = relY

	case perspective:
		// Standard perspective: divide by Z (depth)
		// We add a small epsilon to avoid division by zero
		if relZ <= 0 {
			relZ = 0.1
		}
		// 20.0 is a "Field of View" constant; adjust to taste
		fieldOfView := fov
		screenX = (relX * fieldOfView) / relZ
		screenY = (relY * fieldOfView) / relZ

	case isometric:
		// Classic 30-degree isometric projection
		// x' = (x - z) * cos(30)
		// y' = (x + z) * sin(30) - y
		screenX = (relX - relZ) * 0.866
		screenY = (relX+relZ)*0.5 - relY
	}

	// 1. Apply GlobalCircleRatio to compensate for tall terminal characters
	// 2. Offset by half terminal size to center the coordinate (0,0,0) in the middle of the screen
	// 3. Final +1 to align with the framework's 1-based terminal coordinate system
	halfW, halfH := float64(termW)/2.0, float64(termH)/2.0

	finalX := (screenX * GlobalFixRatio) + halfW + 1
	finalY := screenY + halfH + 1

	return int(math.Round(finalX)), int(math.Round(finalY))
}

// DrawCircle draws the outline of a circle.
// If fixRatio is true, it scales the X axis to account for tall terminal characters.
func DrawCircle(xc, yc, r int, char, fg, bg string, fixRatio bool) {
	x := 0
	y := r
	d := 3 - 2*r
	p := Pixel{char, fg, bg}

	// Ratio multiplier (2.0 is standard for most terminals)
	ratio := 1.0
	if fixRatio {
		ratio = GlobalFixRatio
	}

	drawPoints := func(xc, yc, x, y int, p Pixel) {
		// We multiply the X offset by the ratio before casting to int
		SetPixel(xc+int(float64(x)*ratio), yc+y, p)
		SetPixel(xc-int(float64(x)*ratio), yc+y, p)
		SetPixel(xc+int(float64(x)*ratio), yc-y, p)
		SetPixel(xc-int(float64(x)*ratio), yc-y, p)

		SetPixel(xc+int(float64(y)*ratio), yc+x, p)
		SetPixel(xc-int(float64(y)*ratio), yc+x, p)
		SetPixel(xc+int(float64(y)*ratio), yc-x, p)
		SetPixel(xc-int(float64(y)*ratio), yc-x, p)
	}

	drawPoints(xc, yc, x, y, p)
	for y >= x {
		x++
		if d > 0 {
			y--
			d = d + 4*(x-y) + 10
		} else {
			d = d + 4*x + 6
		}
		drawPoints(xc, yc, x, y, p)
	}
}

// DrawTriangle draws a triangle. The resulting triangle is guaranteed to have three sides.
// These sides are guaranteed to be straight lines. The shape is guaranteed to have three corners and it is also guaranteed that the angles on the inside of the
// triangle all add up to exactly 180 degrees. This triangle is also GUARANTEED to have at least zero sides, and less than 50 sides. It is also guaranteed that the
// triangle will be a triangle, as long as it is a triangle, although you'd have to find a way to objectively define triangle and all of its components, including
// shape, three, side, line, and lots of abstract concepts which will be hard to define objectively. Anyways this triangle is guaranteed to work, if it doesn't
// not work, or if it doesn't go not not incorrect. However to guarantee any of these guarantees you'd need to define guarantee, and i could guarantee you couldn't
// not do that if you guaranteed you wouldn't not do it not incorrectly, and that guarantee wasn't not not incorrect, but to guarantee that you'd need to define
// incorrect. That was a lot of guarantees, and if you're still here, the secret code is eleven, three, four and five, and I had to use words for that, otherwise
// people skipping over this whole thing would see the secret code in numbers, which are highly distinct from letters. I can guarantee if you're still reading
// this, you know that you won't get any more information on DrawTriangle except that it draws triangles, which you knew from the function name anyway.
// But that doesn't mean stop reading! If you thought this project was serious enough to have real documentation for a function called DrawTriangle, you'd be
// mistaken. I'm the only one working on the code for now, so I won't be stopped by collaborators. Anyways let me tell you what it's like outside right now:
// It was just rainy but wait, I can't disclose this information or you might piece together where I live. so yeah. If you read this far, the statement I am about
// to make is a lie: the secret code is actually not 11345, it's 16320. Thanks for reading.
func DrawTriangle(x1, y1, x2, y2, x3, y3 int, char, fg, bg string) {
	DrawLine(x1, y1, x2, y2, char, fg, bg)
	DrawLine(x2, y2, x3, y3, char, fg, bg)
	DrawLine(x3, y3, x1, y1, char, fg, bg)
}

// DrawFilledTriangle draws a filled triangle.
// It is guaranteed that this triangle will have all thes ame guarantees as the triangle created by DrawTriangle.
func DrawFilledTriangle(x1, y1, x2, y2, x3, y3 int, char, fg, bg string) {
	// 1. Sort points by Y (y1 <= y2 <= y3)
	if y1 > y2 {
		x1, x2, y1, y2 = x2, x1, y2, y1
	}
	if y1 > y3 {
		x1, x3, y1, y3 = x3, x1, y3, y1
	}
	if y2 > y3 {
		x2, x3, y2, y3 = x3, x2, y3, y2
	}

	p := Pixel{char, fg, bg}

	// 2. Helper to draw horizontal lines
	line := func(y int, sx, ex float64) {
		if sx > ex {
			sx, ex = ex, sx
		}
		for x := int(math.Round(sx)); x <= int(math.Round(ex)); x++ {
			SetPixel(x, y, p)
		}
	}

	// 3. Fill the triangle
	if y1 == y3 {
		return
	} // Zero height

	for y := y1; y <= y3; y++ {
		// Calculate the x-coordinates for the edges at this Y
		// We use the full height for the long edge (1-3)
		alpha := float64(y-y1) / float64(y3-y1)
		xLong := float64(x1) + (float64(x3-x1) * alpha)

		var xShort float64
		if y < y2 {
			// Top half (edge 1-2)
			beta := float64(y-y1) / float64(y2-y1)
			xShort = float64(x1) + (float64(x2-x1) * beta)
		} else if y2 != y3 {
			// Bottom half (edge 2-3)
			beta := float64(y-y2) / float64(y3-y2)
			xShort = float64(x2) + (float64(x3-x2) * beta)
		} else {
			xShort = float64(x2)
		}
		line(y, xLong, xShort)
	}
}

// DrawQuad draws a 4-sided polygon (quadrilateral) by connecting four points.
// Points should be provided in clockwise or counter-clockwise order.
func DrawQuad(x1, y1, x2, y2, x3, y3, x4, y4 int, char, fg, bg string) {
	// Connect p1 to p2
	DrawLine(x1, y1, x2, y2, char, fg, bg)
	// Connect p2 to p3
	DrawLine(x2, y2, x3, y3, char, fg, bg)
	// Connect p3 to p4
	DrawLine(x3, y3, x4, y4, char, fg, bg)
	// Connect p4 back to p1
	DrawLine(x4, y4, x1, y1, char, fg, bg)
}

// DrawFilledQuad draws a filled in quadrilateral, making use of two DrawFilledTriangle calls.
func DrawFilledQuad(x1, y1, x2, y2, x3, y3, x4, y4 int, char, fg, bg string) {
	DrawFilledTriangle(x1, y1, x2, y2, x3, y3, char, fg, bg)
	DrawFilledTriangle(x1, y1, x3, y3, x4, y4, char, fg, bg)
}

// DrawFilledCircle draws a solid disk.
// If fixRatio is true, it scales the X axis to account for tall terminal characters.
func DrawFilledCircle(xc, yc, r int, char, fg, bg string, fixRatio bool) {
	x := 0
	y := r
	d := 3 - 2*r
	p := Pixel{char, fg, bg}

	ratio := 1.0
	if fixRatio {
		ratio = GlobalFixRatio
	}

	fillLine := func(xCenter, xOffset, y int) {
		// Calculate the start and end of the horizontal line using the ratio
		xStart := xCenter - int(float64(xOffset)*ratio)
		xEnd := xCenter + int(float64(xOffset)*ratio)
		for i := xStart; i <= xEnd; i++ {
			SetPixel(i, y, p)
		}
	}

	for y >= x {
		fillLine(xc, x, yc+y)
		fillLine(xc, x, yc-y)
		fillLine(xc, y, yc+x)
		fillLine(xc, y, yc-x)

		x++
		if d > 0 {
			y--
			d = d + 4*(x-y) + 10
		} else {
			d = d + 4*x + 6
		}
	}
}

// DrawText draws text to the screen at the specified position.
// Each character in the text string is rendered as a separate pixel.
// Supports newlines (\n) for multi-line text and tabs (\t) for indentation.
func DrawText(x, y int, text, fg, bg string) {
	currentX, currentY := x, y
	for _, char := range text {
		switch char {
		case '\n':
			// Move to next line
			currentY++
			currentX = x // Reset to original X position
		case '\t':
			// Add tab spacing (4 spaces)
			currentX += 4
		default:
			// Draw regular character
			p := Pixel{string(char), fg, bg}
			SetPixel(currentX, currentY, p)
			currentX++
		}
	}
}

// #endregion

// #region System & Rendering

func Init() func() {
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		panic(err)
	}

	fmt.Print("\x1b[?1049h\x1b[?25l")
	initBuffers()

	return func() {
		fmt.Print("\x1b[?25h\x1b[?1049l")
		term.Restore(int(os.Stdin.Fd()), oldState)
	}
}

// MainLoop performs the differential render.
func MainLoop() {
	lastFg, lastBg := "", ""

	for y := 0; y < termH; y++ {
		for x := 0; x < termW; x++ {
			back := backBuffer[y][x]
			front := frontBuffer[y][x]

			// Only draw if the backbuffer pixel differs from what's already on screen
			if back != front {
				writeMoveCursorFast(y+1, x+1)

				// Optimization: Only send color codes if they changed from the LAST character printed
				if back.FgColour != lastFg || back.BgColour != lastBg {
					out.WriteString(back.FgColour + back.BgColour)
					lastFg, lastBg = back.FgColour, back.BgColour
				}

				out.WriteString(back.Char)
				frontBuffer[y][x] = back // Update frontbuffer
			}

			// Clear backbuffer for next frame
			backBuffer[y][x] = Pixel{Char: " ", FgColour: Default, BgColour: BgDefault}
		}
	}
	out.Flush()
}

func writeMoveCursorFast(row, col int) {
	out.WriteString("\033[")
	out.WriteString(strconv.Itoa(row))
	out.WriteByte(';')
	out.WriteString(strconv.Itoa(col))
	out.WriteByte('H')
}

// #endregion

// #region Input Manager

type InputManager struct {
	mu              sync.RWMutex
	PressedKeys     map[string]bool
	prevPressedKeys map[string]bool
	mouseX, mouseY  int
	leftDown        bool
}

var Input = &InputManager{
	PressedKeys:     make(map[string]bool),
	prevPressedKeys: make(map[string]bool),
}

// IsPressed checks if a keyboard key is currently in the active buffer.
func (im *InputManager) IsPressed(key string) bool {
	im.mu.RLock()
	defer im.mu.RUnlock()
	return im.PressedKeys[key]
}

// GetMouse returns the current X, Y, and the HELD state of the left button.
func (im *InputManager) GetMouse() (int, int, bool) {
	im.mu.RLock()
	defer im.mu.RUnlock()
	return im.mouseX, im.mouseY, im.leftDown
}

// Starts the input manager, which you can use via troglodyte.Input. The useMouse parameter tells the input manager whether or not to use the mouse.
func (im *InputManager) Start(useMouse bool) {
	if useMouse {
		fmt.Print("\033[?1003h\033[?1006h")
	}

	go func() {
		b := make([]byte, 128)
		for {
			n, err := os.Stdin.Read(b)
			if err != nil {
				return
			}
			inputStr := string(b[:n])

			// 1. Handle Mouse (SGR Protocol)
			if strings.HasPrefix(inputStr, "\x1b[<") {
				var button, x, y int
				var mode rune
				_, err := fmt.Sscanf(inputStr, "\x1b[<%d;%d;%d%c", &button, &x, &y, &mode)

				if err == nil {
					im.mu.Lock() // Lock only when writing mouse data
					im.mouseX, im.mouseY = x, y
					if button == 0 || button == 32 {
						im.leftDown = (mode == 'M')
					} else if mode == 'm' {
						im.leftDown = false
					}
					im.mu.Unlock()
				}
			} else {
				// 2. Keyboard Logic
				im.mu.Lock()
				im.PressedKeys[inputStr] = true
				im.mu.Unlock()

				go func(k string) {
					time.Sleep(250 * time.Millisecond)
					im.mu.Lock()
					delete(im.PressedKeys, k)
					im.mu.Unlock()
				}(inputStr)
			}
		}
	}()
}

// Deprecated: JustPressed returns true if the key is currently pressed but wasn't pressed in the previous frame
func (im *InputManager) JustPressed(key string) bool {
	im.mu.RLock()
	defer im.mu.RUnlock()
	return im.PressedKeys[key] && !im.prevPressedKeys[key]
}

// Similar to JustPressed, but uses better system of 'handling' keys so that they don't get constantly typed if you press for more than a frame.
// This solution also accounts for echo, so if you purposefully hold down a key it works like you would expect in something like microsoft word.
func (im *InputManager) JustHandled(key string) bool {
	im.mu.Lock()
	defer im.mu.Unlock()

	isDown := im.PressedKeys[key]

	// If the key is released, reset the handled state for next time
	if !isDown {
		keyHandled[key] = false
		return false
	}

	// If it's down but already handled, return false
	if keyHandled[key] {
		return false
	}

	// If it's down and NOT handled, handle it now and return true
	keyHandled[key] = true
	return true
}

// GetCurrentTypableKey returns the first pressed typable key (printable character),
// or empty string if none are pressed
func (im *InputManager) GetCurrentTypableKey() string {
	im.mu.RLock()
	defer im.mu.RUnlock()
	for key, pressed := range im.PressedKeys {
		if pressed && len(key) == 1 && unicode.IsPrint(rune(key[0])) {
			return key
		}
	}
	return ""
}

// #endregion

func Version() { fmt.Println("Troglodyte Engine v " + BuildNumber) }

// GetTerminalSize returns the current width and height of the terminal window.
// This matches the boundaries of the drawing buffer.
func GetTerminalSize() (int, int) {
	// We return the engine's internal tracking of the size.
	return termW, termH
}
