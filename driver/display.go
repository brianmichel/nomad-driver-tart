package driver

import "fmt"

const (
	minDisplayWidth  = 800
	maxDisplayWidth  = 1920
	minDisplayHeight = 600
	maxDisplayHeight = 1080
)

func formatDisplayResolution(cfg *DisplayConfig) (string, error) {
	if cfg == nil {
		return "", nil
	}

	if cfg.Width <= 0 {
		return "", fmt.Errorf("display.width must be greater than zero")
	}
	if cfg.Height <= 0 {
		return "", fmt.Errorf("display.height must be greater than zero")
	}

	width := clampInt(cfg.Width, minDisplayWidth, maxDisplayWidth)
	height := clampInt(cfg.Height, minDisplayHeight, maxDisplayHeight)

	return fmt.Sprintf("%dx%d", width, height), nil
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
