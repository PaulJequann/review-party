package discovery

import (
	"os"
	"reviewparty/internal/hostrun"
	"testing"
)

func TestMain(m *testing.M) {
	hostrun.Init()
	os.Exit(m.Run())
}
