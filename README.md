# Hydralix

A simple layer-orientation go scraping framework

## Import
```
import "github/hexrabbit/hydralix"
```

## Features

- layer-orientation scraping usage
- easy to use, at the same time highly customizable
- provide some default helper function
- request parallelism
- auto proxy handling
- retry configuration
- auto header spoofing

## Example
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
  crawler := hydra.New("https://nhentai.net/artists/popular", nil)
  crawler.Add(
    hydra.Command(regex1, 4, filter, callback),
    hydra.Command(regex2, 5, nil, nil),
  )
  crawler.SetOutputAsyncCallback(crawler.DefaultDownloader("img", "./img"))
  crawler.Run()
}
```
