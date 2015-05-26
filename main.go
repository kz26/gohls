/*

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU General Public License for more details.

   You should have received a copy of the GNU General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>.

*/

package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/golang/groupcache/lru"
	"github.com/grafov/m3u8"
	// "github.com/kr/pretty"
)

const VERSION = "1.0.6"

var (
	USER_AGENT     = "Mozilla/5.0 (X11; Linux x86_64; rv:38.0) Gecko/38.0 Firefox/38.0"
	Client         = &http.Client{}
	IV_placeholder = []byte{0, 0, 0, 0, 0, 0, 0, 0}
)

func DoRequest(c *http.Client, req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", USER_AGENT)
	req.Header.Set("Connection", "Keep-Alive")
	resp, err := c.Do(req)

	// Maybe in the future it will force connection to stay opened for "Connection: close"
	resp.Close = false
	resp.Request.Close = false

	return resp, err
}

type Download struct {
	URI           string
	SeqNo         uint64
	ExtXKey       *m3u8.Key
	totalDuration time.Duration
}

func DecryptData(data []byte, v *Download, aes128Keys *map[string][]byte) {
	var (
		iv          *bytes.Buffer
		keyData     []byte
		cipherBlock cipher.Block
	)

	if v.ExtXKey != nil && (v.ExtXKey.Method == "AES-128" || v.ExtXKey.Method == "aes-128") {

		keyData = (*aes128Keys)[v.ExtXKey.URI]

		if keyData == nil {
			req, _ := http.NewRequest("GET", v.ExtXKey.URI, nil)
			resp, _ := DoRequest(Client, req)
			keyData, _ = ioutil.ReadAll(resp.Body)
			resp.Body.Close()
			(*aes128Keys)[v.ExtXKey.URI] = keyData
		}

		if v.ExtXKey.IV == "" {
			iv = bytes.NewBuffer(IV_placeholder)
			binary.Write(iv, binary.BigEndian, v.SeqNo)
		} else {
			iv = bytes.NewBufferString(v.ExtXKey.IV)
		}

		cipherBlock, _ = aes.NewCipher((*aes128Keys)[v.ExtXKey.URI])
		cipher.NewCBCDecrypter(cipherBlock, iv.Bytes()).CryptBlocks(data, data)
	}

}

func DownloadSegment(fn string, dlc chan *Download, recTime time.Duration) {
	var out, err = os.Create(fn)
	defer out.Close()

	if err != nil {
		log.Fatal(err)
		return
	}
	var (
		data       []byte
		aes128Keys = &map[string][]byte{}
	)

	for v := range dlc {
		req, err := http.NewRequest("GET", v.URI, nil)
		if err != nil {
			log.Fatal(err)
		}
		resp, err := DoRequest(Client, req)
		if err != nil {
			log.Print(err)
			continue
		}
		if resp.StatusCode != 200 {
			log.Printf("Received HTTP %v for %v\n", resp.StatusCode, v.URI)
			continue
		}

		data, _ = ioutil.ReadAll(resp.Body)
		resp.Body.Close()

		DecryptData(data, v, aes128Keys)

		_, err = out.Write(data)

		// _, err = io.Copy(out, resp.Body)
		if err != nil {
			log.Fatal(err)
		}

		log.Printf("Downloaded %v\n", v.URI)
		if recTime != 0 {
			log.Printf("Recorded %v of %v\n", v.totalDuration, recTime)
		} else {
			log.Printf("Recorded %v\n", v.totalDuration)
		}
	}
}

func GetPlaylist(urlStr string, recTime time.Duration, useLocalTime bool, dlc chan *Download) {
	startTime := time.Now()
	var recDuration time.Duration = 0
	cache := lru.New(1024)
	playlistUrl, err := url.Parse(urlStr)
	if err != nil {
		log.Fatal(err)
	}
	for {
		req, err := http.NewRequest("GET", urlStr, nil)
		if err != nil {
			log.Fatal(err)
		}
		resp, err := DoRequest(Client, req)
		if err != nil {
			log.Print(err)
			time.Sleep(time.Duration(3) * time.Second)
		}
		playlist, listType, err := m3u8.DecodeFrom(resp.Body, true)
		if err != nil {
			log.Fatal(err)
		}
		resp.Body.Close()
		if listType == m3u8.MEDIA {
			mpl := playlist.(*m3u8.MediaPlaylist)

			for segmentIndex, v := range mpl.Segments {
				if v != nil {
					var msURI string
					if strings.HasPrefix(v.URI, "http") {
						msURI, err = url.QueryUnescape(v.URI)
						if err != nil {
							log.Fatal(err)
						}
					} else {
						msUrl, err := playlistUrl.Parse(v.URI)
						if err != nil {
							log.Print(err)
							continue
						}
						msURI, err = url.QueryUnescape(msUrl.String())
						if err != nil {
							log.Fatal(err)
						}
					}
					_, hit := cache.Get(msURI)
					if !hit {
						cache.Add(msURI, nil)
						if useLocalTime {
							recDuration = time.Now().Sub(startTime)
						} else {
							recDuration += time.Duration(int64(v.Duration * 1000000000))
						}
						dlc <- &Download{
							URI:           msURI,
							ExtXKey:       mpl.Key,
							SeqNo:         uint64(segmentIndex) + mpl.SeqNo,
							totalDuration: recDuration,
						}
					}
					if recTime != 0 && recDuration != 0 && recDuration >= recTime {
						close(dlc)
						return
					}
				}
			}
			if mpl.Closed {
				close(dlc)
				return
			} else {
				time.Sleep(time.Duration(int64(mpl.TargetDuration * 1000000000)))
			}
		} else {
			log.Fatal("Not a valid media playlist")
		}
	}
}

func main() {

	duration := flag.Duration("t", time.Duration(0), "Recording duration (0 == infinite)")
	useLocalTime := flag.Bool("l", false, "Use local time to track duration instead of supplied metadata")
	flag.StringVar(&USER_AGENT, "ua", fmt.Sprintf("gohls/%v", VERSION), "User-Agent for HTTP Client")
	flag.Parse()

	os.Stderr.Write([]byte(fmt.Sprintf("gohls %v - HTTP Live Streaming (HLS) downloader\n", VERSION)))
	os.Stderr.Write([]byte("Copyright (C) 2013 GoHLS Authors. Licensed for use under the GNU GPL version 3.\n"))

	if flag.NArg() < 2 {
		os.Stderr.Write([]byte("Usage: gohls [-l=bool] [-t duration] [-ua user-agent] media-playlist-url output-file\n"))
		flag.PrintDefaults()
		os.Exit(2)
	}

	if !strings.HasPrefix(flag.Arg(0), "http") {
		log.Fatal("Media playlist url must begin with http/https")
	}

	msChan := make(chan *Download, 1024)
	go GetPlaylist(flag.Arg(0), *duration, *useLocalTime, msChan)
	DownloadSegment(flag.Arg(1), msChan, *duration)
}
