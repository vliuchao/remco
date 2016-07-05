package template

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io/ioutil"
	"path"

	"golang.org/x/crypto/openpgp"
)

func appendPrefix(prefix string, keys []string) []string {
	s := make([]string, len(keys))
	for i, k := range keys {
		s[i] = path.Join(prefix, k)
	}
	return s
}

func decrypt(data string, entityList openpgp.EntityList) (string, error) {
	// Taken from crypt and adapted
	decoder := base64.NewDecoder(base64.StdEncoding, bytes.NewBufferString(data))
	md, err := openpgp.ReadMessage(decoder, entityList, nil, nil)
	if err != nil {
		return data, err
	}
	gzReader, err := gzip.NewReader(md.UnverifiedBody)
	if err != nil {
		return data, err
	}
	defer gzReader.Close()
	bytes, err := ioutil.ReadAll(gzReader)
	if err != nil {
		return data, err
	}
	return string(bytes), nil
}
