package main

import "flag"
import "fmt"
import "io"
import "net/http"
import "log"
import "os"
import "time"
import "lru" // https://github.com/golang/groupcache/blob/master/lru/lru.go
import "github.com/grafov/m3u8"

const VERSION = "1.0.0"

const USER_AGENT = "Mozilla/5.0 (Windows NT 6.3; WOW64; rv:26.0) Gecko/20100101 Firefox/26.0"

func doRequest(c *http.Client, req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", USER_AGENT)
	resp, err := c.Do(req)
	return resp, err
}

func downloadSegment(fn string, feed chan m3u8.MediaSegment) {
	out, err := os.Create(fn)
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	client := &http.Client{}
	for v := range feed {
		req, err := http.NewRequest("GET", v.URI, nil)
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
		log.Printf("Downloaded %v\n", v.URI)
	}
}

func getPlaylist(url string, duration time.Duration, useLocalTime bool, feed chan m3u8.MediaSegment) {
	startTime := time.Now()
	var recTime time.Duration
	cache := lru.New(64)
	client := &http.Client{}
	for {
		req, err := http.NewRequest("GET", url, nil)
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
					_, hit := cache.Get(v.URI)
					if !hit {
						feed <- *v
						cache.Add(v.URI, nil)
						log.Printf("Queued %v\n", v.URI)
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
				time.Sleep(time.Duration(int(mpl.TargetDuration)) * time.Second)
			}
		} else {
			log.Fatal("Not a valid media playlist")
		}
	}
}

func main() {

	duration := flag.Duration("t", time.Duration(0), "Recording duration (0 == infinite)")
	useLocalTime := flag.Bool("l", false, "Use local time to track duration instead of supplied metadata")
	flag.Parse()

	os.Stderr.Write([]byte(fmt.Sprintf("gohls %v - HTTP Live Streaming (HLS) downloader\n", VERSION)))
	os.Stderr.Write([]byte("Copyright (C) 2013 Kevin Zhang. Licensed for use under the GNU GPL version 3.\n"))

	if flag.NArg() < 2 {
		os.Stderr.Write([]byte("Usage: gohls [-t duration] [-l=bool] media-playlist-url output-file\n"))
		flag.PrintDefaults()
		os.Exit(2)
	}

	msChan := make(chan m3u8.MediaSegment, 64)
	go getPlaylist(flag.Arg(0), *duration, *useLocalTime, msChan)
	downloadSegment(flag.Arg(1), msChan)
}