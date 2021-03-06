// cumuli - A followings visualizer for SoundCloud

// To do:
//  - Add better logging
//  - Add dynamic resizing w/o refresh
//  - Patch MainHandler getting called 2/3 times
//  - Add a search bar
//      - http://jsbin.com/iyewas/73/edit?html,js,output
//      - http://blogs.msdn.com/b/murads/archive/2013/02/20/using-jquery-ui-autocomplete-with-the-rest-api-to-get-search-results-in-the-search-box.aspx
//      - http://stackoverflow.com/questions/14083272/how-to-make-a-tags-box-using-jquery-with-text-input-field-tags-separated-by

package main 

import (
    "html/template"
    "io/ioutil"
    "log"
    "net/http"
    "net/url"
    "os"
    "path"

    "github.com/garyburd/redigo/redis"
    "github.com/lkvnstrs/cumuli/networkmapper"
)

const TEMPLATES_DIR = `./templates`

var (
    n networkmapper.NetworkMapper
    pool *redis.Pool
)

func init() {

    // Set log flags
    log.SetFlags(log.LstdFlags | log.Lmicroseconds)

    // Load templates
    loadTemplates()

    // Get the SoundCloud client Id
    clientId := GetClientId()

     // Initialize the pool
    redisServer, redisPassword := GetRedisInfo()
    pool = NewPool(redisServer, redisPassword)

    // Initialize the networker
    numResults := 50
    n = networkmapper.NewNetworkMapper(clientId, numResults)

    // Routes
    http.HandleFunc("/", MainHandler)
    http.HandleFunc("/u/", UserHandler)
    http.HandleFunc("/about/", AboutHandler)
    http.HandleFunc("/json/", JSONHandler)
    http.HandleFunc("/static/", StaticHandler)    
}

func main() {

    // Get the web port
    port := GetWebPort()

    // Defer close for the networker
    defer pool.Close()

    log.Println("Running on port ", port)
    http.ListenAndServe(port, nil)

}

// loadTemplates loads all of the templates in TEMPLATES_DIR to be served.
func loadTemplates() {
    if templates == nil {
        templates = make(map[string]*template.Template)
    }

    // Import each file as an extension of base.html.
    files, _ := ioutil.ReadDir(TEMPLATES_DIR)
    base := path.Join(TEMPLATES_DIR, "base.html")

    for _, f := range files {
        if f.Name() != "base.html" {
            mainPath := path.Join(TEMPLATES_DIR, f.Name())
            templates[f.Name()] = template.Must(template.ParseFiles(mainPath, base))
        }
    }
}

// GetPort gets a PORT env if set and returns 8080 otherwise.
func GetWebPort() string {
        var port = os.Getenv("PORT")
        // Set a default port if there is nothing in the environment
        if port == "" {
                port = "8080"
                log.Println("INFO: No PORT environment variable detected, defaulting to " + port)
        }
        return ":" + port
}

// GetClientId gets the Soundcloud API client id.
func GetClientId() string {
    cid := os.Getenv("SC_CLIENT_ID")
    if cid == "" {
        log.Fatal("You forgot SC_CLIENT_ID")
    }
    return cid
}

// GetRedisInfo gets the port and password for the Redis database
func GetRedisInfo() (string, string) {

    var redisUrl = os.Getenv("REDISTOGO_URL")
    if redisUrl == "" {
        return ":6379", ""
    }

    redisInfo, _ := url.Parse(redisUrl)
    server := redisInfo.Host
    password := ""
    if redisInfo.User != nil {
    password, _ = redisInfo.User.Password()
    }

    return server, password
}
