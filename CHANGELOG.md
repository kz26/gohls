1.0.5 - 11/06/2014
* Fix premature termination when downloading VOD streams with t=0
* Hide total recording time in log output when t=0
* Use forked version of m3u8 to fix EXT-X-PLAYLIST-TYPE parsing bug

1.0.4 - 12/30/2013

* Handle non-200 HTTP responses in downloadSegment()

1.0.3 - 12/25/2013

* Improve logging
* Fix over-recording bug
* Unescape all URLs to reduce risk of duplicate segment downloads (e.g. China Central TV streams)

1.0.2 - 12/25/2013

* Bypass URL parsing for absolute media segment URLs
* Change default user agent to gohls/VERSION

1.0.1 - 12/23/2013

* Fix duplicate segment download bug
* Use more precise TargetDuration translation

1.0.0 - 12/23/2013

* Initial release