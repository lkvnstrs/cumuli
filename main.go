// Cumuli - A followings visualizer for SoundCloud

// To do:
//  - Eliminate link duplicates ()
//  - Add http-based error handling
//  - Add weight-based circle radius
//  - Hide static directory listings 

package main 

import (
    "encoding/json"
    "html/template"
    "io/ioutil"
    "log"
    "math"
    "net/http"
    "os"
    "path"
    "strconv"
    "strings"
    "sync"
    "time"
)

const EXPIRE_TIME = 60 // Expiration time for the static JSON files.
const NUM_RESULTS = 50 // Number of results returned in each SoundCloud query.

// A type for the final JSON result.
type Result struct {
    Nodes []Node `json:"nodes"`
    Links []Link `json:"links"`
}

// A type for each node.
type Node struct {
    Name string `json:"name"`
    Group int `json:"group"`
}

// A type for each link.
type Link struct {
    Source int `json:"source"`
    Target int `json:"target"`
}

// A type for a user's followings.
type Followings struct {
    Whoms []string
    Who string
}

// Types for soundcloud unmarshaling.
type scUser struct { FollowingCount float64  `json:"followings_count"` }
type scFollowing struct { Permalink string `json:"permalink"`}

func main() {
    // Routes
    http.HandleFunc("/", MainHandler)
    http.HandleFunc("/static/", StaticHandler)

    log.Println("Running on port :8080")
    http.ListenAndServe(":8080", nil)
}

// MainHandler handles the route '/'.
func MainHandler(rw http.ResponseWriter, r *http.Request) {

    // Get the query parameter for u
    uParam := r.FormValue("u")

    if uParam == "" {
        rw.Write([]byte("Gotta handle this"))
        return
    }

    // Check if the JSON file already exists
    filename := `./static/json/` + uParam + `.json`

    // Handle file doesn't exist
    if _, err := os.Stat(filename); os.IsNotExist(err) {

        // Split fParam into individual users
        users := strings.Split(uParam, " ")

        // Get the shared followings among the users
        result := GetSharedFollowings(&users)

        // JSON marshal the result
        out, err := json.Marshal(*result)
        if err != nil {
            http.Error(rw, err.Error(), http.StatusInternalServerError)
            return
        }

        // Create a file <uParam>.json
        f, err := os.Create(filename)
        if err != nil {
            http.Error(rw, err.Error(), http.StatusInternalServerError)
            return
        }
        defer f.Close()

        // Store the JSON in it
        _, err = f.Write(out)
        if err != nil {
            http.Error(rw, err.Error(), http.StatusInternalServerError)
            return
        }

        // Hold on to the result for EXPIRE_TIME seconds
        // to cover user refreshes
        go func (filename string) {
            time.Sleep(time.Second * EXPIRE_TIME)
            os.Remove(filename)
        } (filename)
    }

    // Create a template for the result
    fp := path.Join("templates", "index.html")
    tmpl, err := template.ParseFiles(fp)
    if err != nil {
        http.Error(rw, err.Error(), http.StatusInternalServerError)
        return
    }

    // Serve the template
    if err := tmpl.ExecuteTemplate(rw, "filename", filename); err != nil {
        http.Error(rw, err.Error(), http.StatusInternalServerError)
    }
}

// StaticHandler handles the static assets of the app.
func StaticHandler(rw http.ResponseWriter, r *http.Request) {
    http.ServeFile(rw, r, r.URL.Path[1:])
}

// GetAllFollowings returns a channel of Followings objects for the 
// given users.
// A channel is used to concurrently handle the calls to GetFollowings.
func GetAllFollowings(users []string) (<-chan Followings) {

    // Create a channel for the followings
    cf := make(chan Followings)
    
    // Iterate over the users and pass their
    // followings onto channel
    go func() {
        var wg sync.WaitGroup

        // GetFollowings for each user
        for _, u := range users {

            wg.Add(1)
            go func(u string) {
                cf <- Followings{Whoms: GetFollowings(u), Who:u}
                wg.Done()
            } (u)
        }

        wg.Wait()
        close(cf)
    } ()

    return cf

}

// GetFollowings returns a slice of strings containing the usernames of 
// the followings of the provided user.
func GetFollowings(user string) ([]string) {

    var url string

    // Get the Soundcloud API client id
    clientId := os.Getenv("SC_CLIENT_ID")
    if clientId == "" {
        panic("darn")
    }

    // Get u's number of followings
    url = `http://api.soundcloud.com/users/` + user + `.json?client_id=` + clientId
    r, err := http.Get(url)
    if err != nil {
        panic(err)
    }
    defer r.Body.Close()

    body, err := ioutil.ReadAll(r.Body)
    if err != nil {
        panic(err)
    }

    // user object to store unmarshalled json
    var u scUser
    if err = json.Unmarshal(body, &u); err != nil {
        panic(err)
    }

    jsonFollowings := make([]scFollowing, NUM_RESULTS)
    followings := make([]string, int(u.FollowingCount))

    // Search for the user's followings
    // Iterate to account for the results limit
    countTo := math.Ceil(u.FollowingCount / float64(NUM_RESULTS))
    for i := 0; i < int(countTo); i++ {

        url = `http://api.soundcloud.com/users/` + user + `/followings.json?client_id=` + clientId + `&offset=` + strconv.Itoa(i * 50)
        r, err = http.Get(url)
        if err != nil {
            panic(err)
        }
        defer r.Body.Close()

        body, err = ioutil.ReadAll(r.Body)
        if err != nil {
            panic(err)
        }

        // unmarshal into jsonFollowings
        if err = json.Unmarshal(body, &jsonFollowings); err != nil {
            panic(err)
        }

        for j, jf := range jsonFollowings {
            index := j + (i * NUM_RESULTS)
            if index >= int(u.FollowingCount) {
                break
            }
            followings[index] = jf.Permalink   
        }
    }
    
    return followings
}


// GetSharedFollowings creates a D3-formatted result containing nodes and links for
// all users followed by at least two of the given users.
func GetSharedFollowings(users *[]string) (*Result) {
    
    // Get a channel of Followings for the given users
    cf := GetAllFollowings(*users)

    // Create two sets to handle consolidation of the map
    checkSet := make(map[string]bool)
    resultSet := make(map[string]bool)

    // Create a slice of Followings to check once the resultSet
    // is filled
    followings := make([]Followings, len(*users))

    // Create two slices to hold the nodes and links
    nodes := make([]Node, len(*users))
    links := []Link{}

    // Keep track of node numbers, which 
    // are generated by order
    nodeNums := make(map[string]int)
    nodeCount := 0

    for i, u := range *users {

        // Put each user in the check and results sets
        checkSet[u] = true
        resultSet[u] = true

        // Make a node from each user and 
        // pass it to the channel
        nodes[i] = Node{Name: u, Group: 1} // Group: 1 -> given user
        nodeNums[u] = nodeCount
        nodeCount++
    }

    // Grab the first collection of followings
    followings[0] = <-cf

    // Store first's followings in the set
    for _, f := range followings[0].Whoms {
        checkSet[f] = true
    }

    // Suck the remaining Followings into a hashmap
    fIndex := 1
    for fs := range cf {

        followings[fIndex] = fs

        for _, f := range fs.Whoms {
            if checkSet[f] {
                if !resultSet[f] {
                    // Append a new node onto the slice
                    nodes = append(nodes, Node{Name: f, Group: 2}) // Group: 2 -> following
                    nodeNums[f] = nodeCount
                    nodeCount++
                }
                resultSet[f] = true
            }
            checkSet[f] = true
        }
        fIndex++
    }

    // Iterate through followings checking the set
    for _, fs := range followings {
        for _, f := range fs.Whoms {
            if resultSet[f] {
                // Append a new link to the slice
                links = append(links, Link{Source: nodeNums[fs.Who], Target: nodeNums[f]})
            }
        }
    }

    // Return a pointer to a Result object
    return &Result{Nodes: nodes, Links: links}
}

func noDirListing(h http.Handler) http.HandlerFunc {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if strings.HasSuffix(r.URL.Path, "/") {
            http.NotFound(w, r)
            return
        }
        h.ServeHTTP(w, r)
    })
}