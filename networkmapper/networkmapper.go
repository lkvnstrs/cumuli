// Package network-mapper provides functionality for creating D3-formatted results from
// follower-based networks
package networkmapper

import (
    "encoding/json"
    "io/ioutil"
    "math"
    "net/http"
    "strconv"
    "sync"
)

// A type that satisfies network.NetworkMapper can be used to generate networks
// with this package.
type NetworkMapper interface {

    // Gets the followings of a given user
    GetFollowings(user string) []string 

}

// networkMapper is the implimentation of NetworkMapper.
type networkMapper struct {
    clientId string
    numResults int
}

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

// NewNetworkMapper creates a new NetworkMapper.
func NewNetworkMapper(id string, num int) NetworkMapper {
    return &networkMapper{
        clientId: id,
        numResults: num,
    }
}

// BuildNetwork creates a new network entry in Redis for the given key.
func BuildNetworkMap(n NetworkMapper, users []string) ([]byte, error) {

    var js []byte

    // Filter into shared followings among the users
    result := GetSharedFollowings(n, users[0:])    

    // JSON marshal the result
    js, err := json.Marshal(*result)
    if err != nil {
        return nil, err
    }

    return js[0:], nil
}

// // Types for soundcloud unmarshaling.
// type scUser struct { FollowingCount float64  `json:"followings_count"` }
// type scFollowing struct { Permalink string `json:"permalink"`}

// GetFollowings returns a slice of strings containing the usernames of 
// the followings of the provided user.
func (n *networkMapper) GetFollowings(user string) ([]string) {

    var url string

    // Get u's number of followings
    url = `http://api.soundcloud.com/users/` + user + `.json?client_id=` + n.clientId
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
    var u struct { 
        FollowingCount float64  `json:"followings_count"` 
    }

    if err = json.Unmarshal(body, &u); err != nil {
        panic(err)
    }

    jsonFollowings := make([]struct { Permalink string `json:"permalink"`}, n.numResults)
    followings := make([]string, int(u.FollowingCount))

    // Search for the user's followings
    // Iterate to account for the results limit
    var wg sync.WaitGroup

    countTo := math.Ceil(u.FollowingCount / float64(n.numResults))
    for i := 0; i < int(countTo); i++ {

        wg.Add(1)
        go func(i int) {

            url := `http://api.soundcloud.com/users/` + 
                   user + `/followings.json?client_id=` + 
                   n.clientId + `&offset=` + strconv.Itoa(i * 50)
                   
            r, err := http.Get(url)
            if err != nil {
                panic(err)
            }
            defer r.Body.Close()

            body, err := ioutil.ReadAll(r.Body)
            if err != nil {
                panic(err)
            }

            // unmarshal into jsonFollowings
            if err = json.Unmarshal(body, &jsonFollowings); err != nil {
                panic(err)
            }

            for j, jf := range jsonFollowings {
                index := j + (i * n.numResults)
                if index >= int(u.FollowingCount) {
                    break
                }
                followings[index] = jf.Permalink   
            }

            wg.Done()
        } (i)
    }

    wg.Wait()
    return followings[0:]
}


// GetAllFollowings returns a channel of Followings objects for the 
// given users.
// A channel is used to concurrently handle the calls to GetFollowings.
func GetAllFollowings(n NetworkMapper, users []string) (<-chan Followings) {

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
                cf <- Followings{Whoms: n.GetFollowings(u), Who:u}
                wg.Done()
            } (u)
        }

        wg.Wait()
        close(cf)
    } ()

    return cf
}

// GetSharedFollowings creates a Result containing nodes and links for
// all users followed by at least two of the given users.
func GetSharedFollowings(n NetworkMapper, users []string) (*Result) {

    // Get a channel of Followings for the given users
    cf := GetAllFollowings(n, users[0:])

    // Create two sets to handle consolidation of the map
    checkSet := make(map[string]bool)
    relevantSet := make(map[string]bool)

    // Create a slice of Followings to check once the relevantSet
    // is filled
    followings := make([]Followings, len(users))

    // Create two slices to hold the nodes and links
    nodes := make([]Node, len(users))

    // Keep track of node numbers, which 
    // are generated by order
    nodeNums := make(map[string]int)
    nodeCount := 0

    for i, u := range users {

        // Put each user in the check and results sets
        checkSet[u] = true
        relevantSet[u] = true

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

            // This checkSet/!relevantSet combo is used to ensure a node only
            // gets created on the second time a following is seen
            if checkSet[f] {
                if !relevantSet[f] {
                    // Append a new node onto the slice
                    if f == "" {
                        continue
                    }
                    nodes = append(nodes, Node{Name: f, Group: 2}) // Group: 2 -> following
                    nodeNums[f] = nodeCount
                    nodeCount++
                }
                relevantSet[f] = true
            }
            checkSet[f] = true
        }
        fIndex++
    }

    links := findLinks(followings[0:], relevantSet, nodeNums)

    // Return a pointer to a Result object
    return &Result{Nodes: nodes, Links: links}
}

// findLinks converts a slice of Followings with relevance specified 
// by the relevantSet into a slice of Links with Nodes numbered by nodeNums
func findLinks(followings []Followings, relevantSet map[string]bool, nodeNums map[string]int) []Link {

    links := []Link{}

    for _, fs := range followings {
        for _, f := range fs.Whoms {
            if relevantSet[f] {

                // Append a new link to the slice
                links = append(links, Link{Source: nodeNums[fs.Who], Target: nodeNums[f]})
            }
        }
    }

    return links[0:]
}