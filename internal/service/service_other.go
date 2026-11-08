//go:build !linux

package service

func init() {
	platformSupported = false
}
