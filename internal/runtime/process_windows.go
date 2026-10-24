package runtime

import (
	"os/exec"
	"time"
)

func boundCommandLifetime(cmd *exec.Cmd) { cmd.WaitDelay = 2 * time.Second }
