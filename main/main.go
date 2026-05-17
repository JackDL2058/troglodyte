package main

import (
	//"fmt"
	"math"
	"time"

	"github.com/JackDL2058/troglodyte"
	//e "github.com/JackDL2058/troglodyte/documentation/exampleScripts"
	//s "github.com/JackDL2058/troglodyte/troglosprite"
)

func main() {
	//e.Web()
	restore := troglodyte.Init()
	defer restore()
	troglodyte.Input.Start(true)

	width, height := troglodyte.GetTerminalSize() // the width and height at the start of the game, won't work if resized.

	cx, cy, cVelX, cVelY, cSize := float64(width)/2, float64(height)/2, 0., 0., 3. // start the ball at the center of the screen
	releaseStrengthMultiplier := 30.
	bounceDecel := 7.
	cs := 50.
	constantSlowdown := cs

	// Move these OUTSIDE the loop so they persist
	mx, my := 0, 0
	prevMx, prevMy := 0, 0
	lc := false
	wasPressed := false // Track the state of the mouse in the previous frame
	troglodyte.GlobalFixRatio = 2.5

	for {
		dt := troglodyte.GetDeltaTime()

		// Store the state from the last frame before updating
		prevMx, prevMy = mx, my
		wasPressed = lc
		w, h := troglodyte.GetTerminalSize()

		// Get new state
		mx, my, lc = troglodyte.Input.GetMouse()

		if lc {
			// Handle dragging
			cVelX, cVelY = 0, 0
			cx, cy = float64(mx), float64(my)
		} else {
			// Check if the button was JUST released (Transition from true to false)
			if wasPressed {
				angle, distance := getMouseMoveDirection(float64(mx), float64(my), float64(prevMx), float64(prevMy))
				cVelX = math.Cos(angle) * distance * releaseStrengthMultiplier
				cVelY = math.Sin(angle) * distance * releaseStrengthMultiplier
			}

			// Friction magnitude
			fric := constantSlowdown * dt

			// X-Axis
			if math.Abs(cVelX) < fric {
				cVelX = 0
			} else {
				cVelX -= math.Copysign(fric, cVelX) // Subtract friction in the direction of movement
			}

			// Y-Axis
			if math.Abs(cVelY) < fric {
				cVelY = 0
			} else {
				cVelY -= math.Copysign(fric, cVelY)
			}

			// Always apply velocity if not holding the ball
			cx += cVelX * dt
			cy += cVelY * dt
		}

		// Check collision

		// Hit TOP
		if cy-cSize <= 0 {
			cy = cSize              // Snap to edge
			cVelY = math.Abs(cVelY) // Force direction to be positive (down)
			if cVelY > bounceDecel {
				cVelY -= bounceDecel
			}
		}

		// Hit BOTTOM
		if cy+cSize >= float64(h) {
			cy = float64(h) - cSize  // Snap to edge
			cVelY = -math.Abs(cVelY) // Force direction to be negative (up)
			if math.Abs(cVelY) > bounceDecel {
				cVelY += bounceDecel
			}
		}

		// Hit RIGHT
		if cx+cSize >= float64(w) {
			cx = float64(w) - cSize  // Snap to edge
			cVelX = -math.Abs(cVelX) // Force direction to be negative (left)
			if math.Abs(cVelX) > bounceDecel {
				cVelX += bounceDecel
			}
		}

		// Hit LEFT
		if cx-cSize <= 0 {
			cx = cSize              // Snap to edge
			cVelX = math.Abs(cVelX) // Force direction to be positive (right)
			if cVelX > bounceDecel {
				cVelX -= bounceDecel
			}
		}

		// more boosts to prevent going off screen on top or bottom
		if cy >= float64(h) {
			cVelY -= 0.5
		}
		if cy <= 0 {
			cVelY += 0.5
		}
		if cx >= float64(w) {
			cVelX -= 0.5
		}
		if cx <= 0 {
			cVelX += 0.5
		}

		troglodyte.DrawTitle(1,1,"",troglodyte.Default,troglodyte.BgBlue)
		if troglodyte.Input.JustHandled("k"){
			troglodyte.DrawTitle(1,1,"You pressed K!",troglodyte.Default,troglodyte.BgBlue)
		}
		troglodyte.DrawFilledCircle(int(cx), int(cy), int(cSize), "▒", troglodyte.Cyan, "", true)
		troglodyte.MainLoop()
		time.Sleep(16 * time.Millisecond)
	}
}

// returns an angle in radians of the direction the mouse is moving, and also the distance it moved.
func getMouseMoveDirection(mx, my, prevMx, prevMy float64) (float64, float64) {
	var relY float64 = my - prevMy
	var relX float64 = mx - prevMx                                          // calculate relative x and y
	return math.Atan2(relY, relX), math.Sqrt((relY * relY) + (relX * relX)) // calculate angle and distance
}
