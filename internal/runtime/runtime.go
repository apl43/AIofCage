// Package runtime owns sandbox lifecycles in the dedicated Colima VM. It must never fall back to
// running agents on the host.
package runtime

const (
	ColimaProfile = "aiofcage"
	DockerContext = "colima-aiofcage"
)

type Check struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type Report struct {
	Ready  bool    `json:"ready"`
	Checks []Check `json:"checks"`
}
