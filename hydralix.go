package hydralix

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
  "sync"
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
 * @param {int} index of data (of the same regex match)
 * @param {string} cmd_callback processed data
 * @return {bool} choose or not
 */
type cmd_filter func(int, string) bool

type output_async_callback func(string, *sync.Mutex)
type output_sync_callback func([]string)
type output_async_callback_int func(string, *sync.Mutex, *sync.WaitGroup)

type tagMap map[string]*tagTreeNode

type crawl_cmd struct {
  regex *regexp.Regexp
  matchTimes int
  filter cmd_filter
  callback cmd_callback
  flat bool
}

type tagTreeNode struct {
  tag string
  counterMutex sync.Mutex
  counter int
  parent *tagTreeNode
}

type Hydra struct {
  target string
  commandLayer []crawl_cmd
  proxyList []string
  proxyHealth []int
  proxyCounter int
  outputCallback interface{}

  tagTreeRoot tagTreeNode
  tagNodeTable tagMap
}

type job struct {
  url string
  data string
}

func New(target string, proxies []string) *Hydra {
  g := &Hydra{
    target: target,
    proxyList: proxies,
    tagNodeTable: make(tagMap),
    commandLayer: nil,
    outputCallback: nil,
  }
  g.tagNodeTable[target] = &g.tagTreeRoot

  return g
}

func Command(regex string, matchTimes int, filter cmd_filter, callback cmd_callback) crawl_cmd {
  compiled, err := regexp.Compile(regex)
  if (err != nil) {
    panic("Regexp compilation failed")
  }

  return crawl_cmd{compiled, matchTimes, filter, callback, false}
}

func CommandFlat(regex string, matchTimes int, filter cmd_filter, callback cmd_callback) crawl_cmd {
  compiled, err := regexp.Compile(regex)
  if (err != nil) {
    panic("Regexp compilation failed")
  }

  return crawl_cmd{compiled, matchTimes, filter, callback, true}
}

func(hydra *Hydra) Add(cmds ...crawl_cmd) {
  for _, cmd := range cmds {
    hydra.commandLayer = append(hydra.commandLayer, cmd)
  }
}

func(hydra *Hydra) Addlist(cmd []crawl_cmd) {
  hydra.commandLayer = append(hydra.commandLayer, cmd...)
}

func(hydra *Hydra) Setproxies(proxies []string) {
  hydra.proxyList = append(hydra.proxyList, proxies...)
}

func outputCallbackWrapper(callback output_async_callback) output_async_callback_int {
  return func(url string, mutex *sync.Mutex, wg *sync.WaitGroup) {
    callback(url, mutex)
    wg.Done()
  }
}

func(hydra *Hydra) SetOutputAsyncCallback(callback output_async_callback) {
  hydra.outputCallback = outputCallbackWrapper(callback)
}

func(hydra *Hydra) SetOutputSyncCallback(callback output_sync_callback) {
  hydra.outputCallback = callback
}

func(hydra *Hydra) Run() {
  var targetURLs []string
  var crawled []string

  targetURLs = append(targetURLs, hydra.target)

  for i := 0; i < len(hydra.commandLayer); i++ {
    ch := make(chan job, 8)
    cmd := &hydra.commandLayer[i]

    for _, url := range targetURLs {
      go request(url, cmd, ch)
      fmt.Println("[Request]", url)
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
        parentPtr := hydra.tagNodeTable[res.url]
        if _, exist := hydra.tagNodeTable[processed]; exist {
          fmt.Println("[Warning] Fetched duplicate url, output may be corrupted")
        }

        var nodePtr *tagTreeNode
        if cmd.flat == true {
          nodePtr = parentPtr
        } else {
          nodePtr = &tagTreeNode{
            tag: tag,
            parent: parentPtr,
          }
        }

        hydra.tagNodeTable[processed] = nodePtr
      }
    }
    targetURLs = targetURLs[:0]
    targetURLs = append(targetURLs, crawled...)
    crawled = crawled[:0]
  }

  switch callback := hydra.outputCallback.(type) {
  case output_sync_callback:
    callback(targetURLs)

  case output_async_callback_int:
    var wg sync.WaitGroup
    var mutex sync.Mutex

    for _, url := range targetURLs {
      go callback(url, &mutex, &wg)
      wg.Add(1)
    }
    wg.Wait()
  }
}

//func pickProxy(hydra *Hydra) string { }

func request(url string, cmd *crawl_cmd, ch chan job) {

  tr := &http.Transport{
    MaxIdleConns:       10,
    IdleConnTimeout:    30 * time.Second,
    DisableCompression: true,
    //Proxy:              http.ProxyURL(hydra.PickProxy()),
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

  fmt.Println("[Response]", url, res.Status)
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

  /* should be the final layer, so flat == true */
  return crawl_cmd{compiled, -1, nil, callback, true}
}

func buildPath(hydra *Hydra, dir string, url string) (string, int) {
  var reversePath []string
  /* discard the last tag node */
  ptr := hydra.tagNodeTable[url].parent

  ptr.counterMutex.Lock()
  counter := ptr.counter
  ptr.counter++
  ptr.counterMutex.Unlock()


  for ptr != nil {
    reversePath = append(reversePath, ptr.tag)
    ptr = ptr.parent
  }

  path := dir
  for i := len(reversePath)-1; i >= 0; i-- {
    path = filepath.Join(path, reversePath[i])
  }

  return path, counter
}

func(hydra *Hydra) DefaultDownloader(prefix string, path string) output_async_callback {

  _downloader := func(prefix string, path string, url string) {

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

    dirPath, index := buildPath(hydra, path, url)

    if _, err := os.Stat(dirPath); os.IsNotExist(err) {
      os.MkdirAll(dirPath, 0755)
    }

    // Create the file
    file := filepath.Join(dirPath, prefix + strconv.Itoa(index) + fileExtensions[0])
    out, err := os.Create(file)
    if err != nil {
      panic("File creation failure")
    }
    defer out.Close()

    // Write the body to file
    _, err = out.Write(outputData)
    if err != nil {
      panic("File write error")
    }

    fmt.Println("[Savefile]", file)
  }

  callback := func(url string, mutex *sync.Mutex) { _downloader(prefix, path, url) }
  return callback
}
