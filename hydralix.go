package hydralix

import (
  "regexp"
  "io/ioutil"
  "crypto/tls"
  neturl "net/url"
  "os"
  "strconv"
  "net/http"
  "mime"
  "fmt"
  "sync"
  "bufio"
  "path/filepath"
  "crypto/md5"
  "encoding/hex"
  "github.com/EDDYCJY/fake-useragent"
)

// add some error handler
/* cmd_callback
 * @param {string} crawled url
 * @param {string} matched group
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

type output_callback_interface interface {}
type cmd_interface interface {}

type tagMap map[string]*tagTreeNode

type crawl_cmd struct {
  regex *regexp.Regexp
  matchTimes int
  filter cmd_filter
  callback cmd_callback
  flat bool
}

type recur_cmd struct {
  match *regexp.Regexp
  collect *regexp.Regexp
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
  commandLayer []cmd_interface
  proxyList []string
  proxyHealth []int
  proxyCounter int
  proxyMutex sync.Mutex
  outputCallback output_callback_interface

  tagTreeRoot tagTreeNode
  tagNodeTable tagMap
}

type job struct {
  url string
  data string
}

func New(target string) *Hydra {
  g := &Hydra{
    target: target,
    proxyList: nil,
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

func CommandRecur(match string, collect string, callback cmd_callback) recur_cmd {
  compiled_match, err := regexp.Compile(match)
  if (err != nil) {
    panic("Regexp compilation failed")
  }

  compiled_collect, err := regexp.Compile(collect)
  if (err != nil) {
    panic("Regexp compilation failed")
  }

  return recur_cmd{compiled_match, compiled_collect, callback, false}
}

func CommandFlat(regex string, matchTimes int, filter cmd_filter, callback cmd_callback) crawl_cmd {
  compiled, err := regexp.Compile(regex)
  if (err != nil) {
    panic("Regexp compilation failed")
  }

  return crawl_cmd{compiled, matchTimes, filter, callback, true}
}

func CommandRecurFlat(match string, collect string, callback cmd_callback) recur_cmd {
  compiled_match, err := regexp.Compile(match)
  if (err != nil) {
    panic("Regexp compilation failed")
  }

  compiled_collect, err := regexp.Compile(collect)
  if (err != nil) {
    panic("Regexp compilation failed")
  }

  return recur_cmd{compiled_match, compiled_collect, callback, true}
}

func(hydra *Hydra) Add(cmds ...cmd_interface) {
  for _, cmd := range cmds {
    hydra.commandLayer = append(hydra.commandLayer, cmd)
  }
}

func(hydra *Hydra) Addlist(cmd []cmd_interface) {
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

  for layer := 0; layer < len(hydra.commandLayer); layer++ {
    switch cmd := hydra.commandLayer[layer].(type) {
    case crawl_cmd:
      ch := make(chan job, 8)

      for _, url := range targetURLs {
        go hydra.request(url, ch)
        fmt.Println("[Request]", url)
      }

      for i := 0; i < len(targetURLs); i++ {
        res := <-ch

        re := cmd.regex
        matched := re.FindAllStringSubmatch(res.data, cmd.matchTimes)

        for matched_idx, lst := range matched {
          /* tag is used for build tree and name the folder 
             should be different from each other
             or the mapping may failed
          */
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

          /* drop duplicated urls to avoid conflict */
          if _, exist := hydra.tagNodeTable[processed]; exist {
            fmt.Println("[Warning] Fetched duplicated url: %s", processed)
            continue
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
          var nodePtr *tagTreeNode

          if cmd.flat {
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

    case recur_cmd:
      var collection []string
      ch := make(chan job, 8)

      for len(targetURLs) > 0 {

        for _, url := range targetURLs {
          go hydra.request(url, ch)
          fmt.Println("[Request]", url)
        }

        for i := 0; i < len(targetURLs); i++ {
          res := <-ch

          match := cmd.match
          matched := match.FindAllStringSubmatch(res.data, -1)

          collect := cmd.collect
          collected := collect.FindAllStringSubmatch(res.data, -1)

          parentPtr := hydra.tagNodeTable[res.url]

          for _, lst := range collected {
            hydra.tagNodeTable[lst[1]] = parentPtr
            collection = append(collection, lst[1])
          }

          for _, lst := range matched {
            /* tag is used for build tree and name the folder 
               should be different from each other
               or the mapping may failed
            */
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


            /* drop duplicated urls to avoid conflict */
            if _, exist := hydra.tagNodeTable[processed]; exist {
              fmt.Println("[Warning] Fetched duplicated url: %s", processed)
              continue
            }

            crawled = append(crawled, processed)

            var nodePtr *tagTreeNode

            if cmd.flat {
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
      targetURLs = append(targetURLs, collection...)
    }
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

func(hydra *Hydra) pickProxy() func(*http.Request) (*neturl.URL, error) {
  hydra.proxyMutex.Lock()
  idx := hydra.proxyCounter % len(hydra.proxyList)
  hydra.proxyCounter++
  hydra.proxyMutex.Unlock()

  proxy := hydra.proxyList[idx]
  httpProxy, err := neturl.Parse(proxy)
  if err != nil {
    panic("Parse proxy failed")
  }

  return http.ProxyURL(httpProxy)
}

func(hydra *Hydra) request(url string, ch chan job) {

  RETRY:
  tr := &http.Transport{
    MaxIdleConns:       10,
    //IdleConnTimeout:    30 * time.Second,
    DisableCompression: true,
    TLSNextProto: make(map[string]func(authority string, c *tls.Conn) http.RoundTripper),
  }

  if hydra.proxyList != nil {
    tr.Proxy = hydra.pickProxy()
  }

  client := &http.Client{
    Transport: tr,
    //Timeout: 8 * time.Second,
  }
  req, err := http.NewRequest("GET", url, nil)

  if err != nil {
    panic("Request initialization failed")
  }

  req.Header.Set("User-Agent", browser.Random())
  req.Header.Set("Content-Type", "text/plain; charset=utf-8")
  req.Header.Set("Accept", "*/*")

  res, err := client.Do(req)

  if err != nil {
    fmt.Println(err)
    goto RETRY
    /* maybe proxy fail */
    //panic("Proxy failed or website is not reachable")
  }
  defer res.Body.Close()


  body, err := ioutil.ReadAll(res.Body)
  if err != nil {
    panic("Unable to read body")
  }

  fmt.Println("[Response]", url, res.Status)
  if res.StatusCode != 200 {
    goto RETRY
  }
  ch <- job{url, string(body)}
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

func DefaultFileWriter(filepath string) output_sync_callback {
  _writer := func(output []string) {

    file, err := os.OpenFile(filepath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, os.ModePerm)
    if err != nil {
      panic("File creation failure")
    }
    defer file.Close()

    writer := bufio.NewWriter(file)
    for _, str := range output {
        writer.WriteString(str + "\n")
    }

    err = writer.Flush()
    if err != nil {
      panic("File write failure")
    }
  }

  return _writer
}

/* TODO: Use callback to substitute hydra instance */
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
