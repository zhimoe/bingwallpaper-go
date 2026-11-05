package fsutil

// ReplaceFile atomically replaces destination with source when the operating
// system supports it. Source and destination must be on the same volume.
func ReplaceFile(source, destination string) error {
	return replaceFile(source, destination)
}
