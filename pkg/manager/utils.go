package manager

import (
	"encoding/json"
	"math/rand"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"
	bolt "go.etcd.io/bbolt"
	"go.uber.org/zap"

	"github.com/criticalstack/e2d/pkg/log"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// shortName returns a shorter, lowercase version of the node name. The intent
// is to make log reading easier.
func shortName(name string) string {
	if len(name) > 5 {
		name = name[:5]
	}
	return strings.ToLower(name)
}

func getExistingNameFromDataDir(path string, peerURL url.URL) (string, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return "", err
	}
	defer db.Close()

	var name string
	err = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("members"))
		if b == nil {
			return errors.New("existing name not found")
		}
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var m struct {
				ID       uint64   `json:"id"`
				Name     string   `json:"name"`
				PeerURLs []string `json:"peerURLs"`
			}
			if err := json.Unmarshal(v, &m); err != nil {
				log.Error("cannot unmarshal etcd member", zap.Error(err))
				continue
			}
			for _, u := range m.PeerURLs {
				if u == peerURL.String() {
					name = m.Name
					return nil
				}
			}
		}
		return errors.New("existing name not found")
	})
	return name, err
}
