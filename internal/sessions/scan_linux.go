//go:build linux

package sessions

func DefaultScan() ([]Session, error) {
	return Scan("/proc")
}
