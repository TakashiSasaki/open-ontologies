// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import "os"

func writePrivateInput(path, content string) error { return os.WriteFile(path, []byte(content), 0600) }
