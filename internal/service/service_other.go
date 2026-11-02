//go:build !windows

package service

func init() {
	platformSupported = false
}
