package extensions

import (
	"fmt"
	"sync"

	pb "github.com/EthanMBoos/openc2-gateway/api/proto"
)

var (
	mu     sync.RWMutex
	codecs = make(map[string]Codec)
)

// Register adds a codec to the registry. Call from your extension package's init().
// Panics if a codec for the same namespace is already registered — collision at
// startup is a programming error, not a runtime condition.
func Register(c Codec) {
	mu.Lock()
	defer mu.Unlock()
	if _, exists := codecs[c.Namespace()]; exists {
		panic(fmt.Sprintf("extensions: codec already registered for namespace %q", c.Namespace()))
	}
	codecs[c.Namespace()] = c
}

// Get returns the codec for a namespace, or nil if not registered.
func Get(namespace string) Codec {
	mu.RLock()
	defer mu.RUnlock()
	return codecs[namespace]
}

// All returns all registered codecs (order unspecified).
func All() []Codec {
	mu.RLock()
	defer mu.RUnlock()
	result := make([]Codec, 0, len(codecs))
	for _, c := range codecs {
		result = append(result, c)
	}
	return result
}

// DecodeAll decodes every extension in a telemetry map.
// Unknown namespaces produce an _error entry rather than failing the whole frame —
// one bad extension shouldn't drop all telemetry.
func DecodeAll(exts map[string]*pb.ExtensionData) map[string]any {
	result := make(map[string]any, len(exts))
	for namespace, ext := range exts {
		codec := Get(namespace)
		if codec == nil {
			result[namespace] = map[string]any{
				"_version": ext.Version,
				"_error":   fmt.Sprintf("unknown extension namespace: %s", namespace),
			}
			continue
		}
		decoded, err := codec.DecodeTelemetry(ext.Version, ext.Payload)
		if err != nil {
			result[namespace] = map[string]any{
				"_version": ext.Version,
				"_error":   err.Error(),
			}
			continue
		}
		decoded["_version"] = ext.Version
		result[namespace] = decoded
	}
	return result
}
