package gocrawler

import (
  "regexp"
  "io/ioutil"
  neturl "net/url"
  "os"
  "strconv"
  "net/http"
  "mime"
  "time"
  "fmt"
  "path/filepath"
  "crypto/md5"
  "encoding/hex"
  "github.com/EDDYCJY/fake-useragent"
)

// add some error handler
/* cmd_callback
 * @param {string} crawled url
 * @param {string} matched group 1
 * @param {string} matched group 2
 * @param ...
 * @return {string} processed data
 */
type cmd_callback func(url string, group []string) (string, string)

/* cmd_filter
 * @param {int} index of data
 * @param {string} cmd_callback processed data
 * @return {bool} choose or not
 */
type cmd_filter func(int, string) bool

type output_callback func([]string) error

type tagMap map[string]*tagTreeNode

type crawl_cmd struct {
  regex *regexp.Regexp
  matchTimes int
  filter cmd_filter
  callback cmd_callback
}

type tagTreeNode struct {
  tag string
  parent *tagTreeNode
}

type Gocrawler struct {
  target string
  commandLayer []crawl_cmd
  proxyList []string
  proxyHealth []int
  proxyCounter int
  outputCallback output_callback

  tagTreeRoot tagTreeNode
  tagNodeTable tagMap
}

type job struct {
  url string
  data string
}

func New(target string, proxies []string) *Gocrawler {
  g := &Gocrawler{
    target: target,
    commandLayer: nil,
    proxyList: proxies,
    proxyHealth: nil,
    proxyCounter: 0,
    outputCallback: nil,
    tagNodeTable: make(tagMap),
  }
  g.tagNodeTable[target] = &g.tagTreeRoot

  return g
}

func Command(regex string, matchTimes int, filter cmd_filter, callback cmd_callback) crawl_cmd {
  compiled, err := regexp.Compile(regex)
  if (err != nil) {
    panic("Regexp compilation failed")
  }

  return crawl_cmd{compiled, matchTimes, filter, callback}
}

func(gcr *Gocrawler) Add(cmds ...crawl_cmd) {
  for _, cmd := range cmds {
    gcr.commandLayer = append(gcr.commandLayer, cmd)
  }
}

func(gcr *Gocrawler) Addlist(cmd []crawl_cmd) {
  gcr.commandLayer = append(gcr.commandLayer, cmd...)
}

func(gcr *Gocrawler) Setproxies(proxies []string) {
  gcr.proxyList = append(gcr.proxyList, proxies...)
}

func(gcr *Gocrawler) SetOutputCallback(callback output_callback) {
  gcr.outputCallback = callback
}

func(gcr *Gocrawler) Run() error {
  var targetURLs []string
  var crawled []string

  targetURLs = append(targetURLs, gcr.target)

  for i := 0; i < len(gcr.commandLayer); i++ {
    ch := make(chan job, 8)
    cmd := &gcr.commandLayer[i]

    for i, url := range targetURLs {
      go request(url, cmd, ch)
      fmt.Println("GO", i)
    }

    for i := 0; i < len(targetURLs); i++ {
      res := <-ch

      re := cmd.regex
      matched := re.FindAllStringSubmatch(res.data, cmd.matchTimes)

      for matched_idx, lst := range matched {
        var processed, tag string

        /* post-process */
        if cmd.callback != nil {
          processed, tag = cmd.callback(res.url, lst[1:])

        } else {
          /* no callback: forward first matched group */
          processed = lst[1]

          hasher := md5.New()
          hasher.Write([]byte(lst[1]))
          tag = hex.EncodeToString(hasher.Sum(nil))
        }

        /* filtering */
        if cmd.filter != nil && cmd.filter(matched_idx, processed) {
          crawled = append(crawled, processed)
        } else if cmd.filter == nil {
          crawled = append(crawled, processed)
        }

        /* build tree with tag */

        /* by use of 
          - res.url: 
              should be used to search the parent tag node
          - processed: 
              used to build map[processed] = tag, point tag node to parent tag node
              
          maybe change map[url(processed)]nodePtr -> map[tag]nodePtr ?
          : but you don't know the tag of download link in the outputCallback function
        */
        parentPtr := gcr.tagNodeTable[res.url]
        if _, exist := gcr.tagNodeTable[processed]; exist {
          fmt.Println("[Warning] Fetched duplicate url, output may be corrupted")
        }
        gcr.tagNodeTable[processed] = &tagTreeNode{
          tag: tag,
          parent: parentPtr,
        }
      }
    }
    targetURLs = targetURLs[:0]
    targetURLs = append(targetURLs, crawled...)
    crawled = crawled[:0]
  }

  return gcr.outputCallback(targetURLs)
}

//func pickProxy(gcr *Gocrawler) string { }

func request(url string, cmd *crawl_cmd, ch chan job) {

  tr := &http.Transport{
    MaxIdleConns:       10,
    IdleConnTimeout:    30 * time.Second,
    DisableCompression: true,
    //Proxy:              http.ProxyURL(gcr.PickProxy()),
  }

  client := &http.Client{ Transport: tr }
  req, err := http.NewRequest("GET", url, nil)

  if err != nil {
    panic("Request initialization failed")
  }

  req.Header.Set("User-Agent", browser.Random())
  req.Header.Set("Content-Type", "text/plain; charset=utf-8")
  req.Header.Set("Accept", "*/*")

  res, err := client.Do(req)

  if err != nil {
    /* maybe proxy fail */
    panic("Proxy failed OR website not reachable")
  }
  defer res.Body.Close()

  body, err := ioutil.ReadAll(res.Body)
  if err != nil {
    panic("Unable to read body")
  }

  ch <- job{url, string(body)}
}

func remove(s []int, i int) []int {
  s[i] = s[len(s)-1]

  return s[:len(s)-1]
}

func FetchImg() crawl_cmd {
  compiled, err := regexp.Compile(`<img src=\"([\S\s]*?)\"`)

  if (err != nil) {
    panic("Regexp compilation failed")
  }

  callback := func(url string, group []string) (string, string) {
    parsedUrl, _ := neturl.Parse(url)
    urlrune := []rune(group[0])
    var modurl string

    if urlrune[0] != '/' {
      modurl = group[0]
    } else {
      modurl = parsedUrl.Scheme + "://" + parsedUrl.Hostname() + group[0]
    }
    return modurl, group[0]
  }

  return crawl_cmd{compiled, -1, nil, callback}
}

func buildPath(gcr *Gocrawler, dir string, url string) string {
  var reversePath []string
  /* discard the last tag node */
  ptr := gcr.tagNodeTable[url].parent

  for ptr != nil {
    reversePath = append(reversePath, ptr.tag)
    ptr = ptr.parent
  }

  path := dir
  for i := len(reversePath)-1; i >= 0; i-- {
    path = filepath.Join(path, reversePath[i])
  }

  return path
}

func(gcr *Gocrawler) DefaultDownloader(prefix string, path string) output_callback {

  _downloader := func(prefix string, path string, urls []string) error {
    for index, url := range urls {

      // Get the data
      res, err := http.Get(url)
      if err != nil {
        panic("Http GET error")
      }

      outputData, err := ioutil.ReadAll(res.Body)
      if err != nil {
        panic("Body read error")
      }
      defer res.Body.Close()

      contentType := http.DetectContentType(outputData)
      fileExtensions, err := mime.ExtensionsByType(contentType)

      if err != nil {
        panic("No matched file extension was found")
      }

      dirPath := buildPath(gcr, path, url)

      fmt.Println(dirPath)

      if _, err := os.Stat(dirPath); os.IsNotExist(err) {
        os.MkdirAll(dirPath, 0755)
      }

      // Create the file
      out, err := os.Create(filepath.Join(dirPath, prefix + strconv.Itoa(index) + fileExtensions[0]))
      if err != nil {
        panic("File creation failure")
      }
      defer out.Close()

      // Write the body to file
      _, err = out.Write(outputData)
      if err != nil {
        panic("File write error")
      }
    }
    return nil
  }

  callback := func(urls []string) error { return _downloader(prefix, path, urls) }
  return callback
}
