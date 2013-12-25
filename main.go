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

import "flag"
import "fmt"
import "io"
import "net/http"
import "net/url"
import "log"
import "os"
import "time"
import "lru" // https://github.com/golang/groupcache/blob/master/lru/lru.go
import "strings"
import "github.com/grafov/m3u8"

const VERSION = "1.0.2"

var USER_AGENT string

var client = &http.Client{}

func doRequest(c *http.Client, req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", USER_AGENT)
	resp, err := c.Do(req)
	return resp, err
}

func downloadSegment(fn string, feed chan string) {
	out, err := os.Create(fn)
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	for v := range feed {
		req, err := http.NewRequest("GET", v, nil)
		if err != nil {
			log.Fatal(err)
		}
		resp, err := doRequest(client, req)
		if err != nil {
			log.Print(err)
			continue
		}
		_, err = io.Copy(out, resp.Body)
		if err != nil {
			log.Fatal(err)
		}
		resp.Body.Close()
		log.Printf("Downloaded %v\n", v)
	}
}

func getPlaylist(urlStr string, duration time.Duration, useLocalTime bool, feed chan string) {
	startTime := time.Now()
	var recTime time.Duration
	cache := lru.New(64)
	playlistUrl, err := url.Parse(urlStr)
	if err != nil {
		log.Fatal(err)
	}
	for {
		req, err := http.NewRequest("GET", urlStr, nil)
		if err != nil {
			log.Fatal(err)
		}
		resp, err := doRequest(client, req)
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
			for _, v := range mpl.Segments {
				if v != nil {
					var msURI string
					if strings.HasPrefix(v.URI, "http") {
						msURI = v.URI
					} else {
						msUrl, err := playlistUrl.Parse(v.URI)
						if err != nil {
							log.Print(err)
							continue
						}
						msURI = msUrl.String() 
					}
					_, hit := cache.Get(msURI)
					if !hit {
						feed <- msURI
						cache.Add(msURI, nil)
						log.Printf("Queued %v\n", msURI)
						if useLocalTime {
							recTime = time.Now().Sub(startTime)
						} else {
							recTime += time.Duration(int64(v.Duration * 1000000000))
						}
						log.Printf("Recorded %v of %v\n", recTime, duration)
					}
				}
				if duration != 0 && recTime > duration  {
					close(feed)
					return
				}
			}
			if mpl.Closed {
					close(feed)
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
	flag.StringVar(&USER_AGENT, "ua", fmt.Sprintf("gohls/%v", VERSION), "User-Agent for HTTP client")
	flag.Parse()

	os.Stderr.Write([]byte(fmt.Sprintf("gohls %v - HTTP Live Streaming (HLS) downloader\n", VERSION)))
	os.Stderr.Write([]byte("Copyright (C) 2013 Kevin Zhang. Licensed for use under the GNU GPL version 3.\n"))

	if flag.NArg() < 2 {
		os.Stderr.Write([]byte("Usage: gohls [-l=bool] [-t duration] [-ua user-agent] media-playlist-url output-file\n"))
		flag.PrintDefaults()
		os.Exit(2)
	}

	msChan := make(chan string, 1024)
	go getPlaylist(flag.Arg(0), *duration, *useLocalTime, msChan)
	downloadSegment(flag.Arg(1), msChan)
}