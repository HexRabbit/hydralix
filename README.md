# Hydralix

A simple layer-orientation go scraping framework

## Import
```
import "github/hexrabbit/hydralix"
```

## Features

- Layer-orientation scraping usage
- Easy to use, at the same time highly customizable
- Provide some default helper function
- Request parallelism
- Auto proxy handling
- Retry configuration
- Auto header spoofing

## Examples

### nhentai
```go
package main

import (
  hydra "github.com/hexrabbit/hydralix"
)


func callback(url string, group []string) (string, string) {
  newurl := "https://nhentai.net/artist/" + group[0]
  return newurl, group[0]
}

func filter(index int, url string) bool {
  if index == 2 {
    return false
  } else {
    return true
  }
}

func main() {
  regex1 := `<a href="/artist/(.*)/"`
  regex2 := `<img is="lazyload-image".*?src="(.*?)"`
  crawler := hydra.New("https://nhentai.net/artists/popular")
  crawler.Add(
    hydra.Command(regex1, 4, filter, callback),
    hydra.Command(regex2, 5, nil, nil),
  )
  crawler.SetOutputAsyncCallback(crawler.DefaultDownloader("img", "./img"))
  crawler.Run()
}
```

### ptt
```go
package main

import (
  hydra "hydralix"
)


func callback(url string, group []string) (string, string) {
  newurl := "https://www.ptt.cc" + group[0]
  return newurl, group[0]
}

func main() {
  match := `<a class="board" href="(/cls/\d*)">`
  collect := `<a class="board" href="/bbs/(.*)/index.html">`
  crawler := hydra.New("https://www.ptt.cc/cls/1")

  crawler.Add(
    hydra.CommandRecurFlat(match, collect, callback),
  )
  crawler.SetOutputSyncCallback(hydra.DefaultFileWriter("./output.txt"))
  crawler.Run()
}
```

## Troubleshooting
If some error looks like this pop out, you can try `ulimit -n 4096` or higher.
```bash
dial tcp: lookup www.ptt.cc on 127.0.1.1:53: dial udp 127.0.1.1:53: socket: too many open files
```
