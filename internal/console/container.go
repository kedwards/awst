package console

import (
	"fmt"
	"hash/fnv"
	"net/url"
)

// containerColors is the fixed Firefox Multi-Account Containers palette the
// awst Containers extension accepts.
var containerColors = []string{"blue", "turquoise", "green", "yellow", "orange", "red", "pink", "purple"}

const defaultContainerIcon = "fingerprint"

// ContainerURL wraps target in the ext+awst-containers custom protocol so the
// awst Containers Firefox extension opens it in an isolated, named container.
// The color is derived deterministically from name, so each profile keeps a
// stable, distinct color across runs.
func ContainerURL(name, target string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	color := containerColors[h.Sum32()%uint32(len(containerColors))]
	return fmt.Sprintf("ext+awst-containers:name=%s&url=%s&color=%s&icon=%s",
		name, url.QueryEscape(target), color, defaultContainerIcon)
}
