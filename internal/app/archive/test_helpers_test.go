package archive

import "os"

func writeTestFile(name string) error {
	return os.WriteFile(name, []byte("existing"), 0600)
}
