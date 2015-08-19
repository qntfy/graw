// Package monitor continuously updates monitored sections of reddit, such as
// subreddits and threads.
package monitor

import (
	"bytes"
	"container/list"
	"sync"

	"github.com/turnage/graw/bot/internal/operator"
	"github.com/turnage/redditproto"
)

const (
	// maxPosts is the maximum number of posts to request new at once.
	maxPosts = 100
	// maxTipSize is the number of posts to keep in the tracked tip. More than
	// one is kept because a tip is needed to fetch only posts newer than
	// that post. If one is deleted, monitor moves to a fallback tip.
	maxTipSize = 15
)

// Monitor monitors sections of reddit real time and exports updates. All
// methods and channels of Monitor expect that Run() is alive in a goroutine.
// Calling them when that condition is not true is not defined behavior.
type Monitor struct {
	// NewPosts provides new posts to monitored subreddits. These posts will
	// have been posted very recently so they probably won't have comments
	// or votes yet.
	NewPosts chan *redditproto.Link
	// PostUpdates provides updated versions of watched threads. There is no
	// gauruntee that a thread, once updated, will contain new information;
	// it is possible no new activity occurs between updates.
	PostUpdates chan *redditproto.Link

	// op is the operator through which the monitor will make update
	// requests to reddit.
	op *operator.Operator
	// tip is the list of latest posts in the monitored subreddits.
	tip *list.List

	// mu protects the following fields.
	mu sync.Mutex
	// monitoredSubreddits is the list of monitored subreddits from which
	// the requests are built.
	monitoredSubreddits map[string]bool
	// monitoredThreads is the list of monitored threads from which the
	// requests are built.
	monitoredThreads map[string]bool
	// subredditQuery is the subreddit alias Monitor uses to fetch new
	// posts. It uses reddit's "+" multireddit technique, e.g. "self+aww".
	subredditQuery string
	// threadQuery is the thread id string Monitor uses to fetch threads. It
	// uses a list of fullnames, e.g. "t3_sdfnd,t3_sdjfdjf".
	threadQuery string
}

// Run is expected to be spawned as a goroutine, and run continuously.
// It is the main loop of the monitor, and output is fed through
// Monitor's exported channels.
func (m *Monitor) Run() {}

// MonitorSubreddits starts monitoring the requested subreddits.
func (m *Monitor) MonitorSubreddits(subreddits ...string) {
	m.mu.Lock()
	setKeys(m.monitoredSubreddits, true, subreddits)
	m.subredditQuery = buildQuery(m.monitoredSubreddits, "+")
	m.mu.Unlock()
}

// UnmonitorSubreddits stops monitoring the requested subreddits.
func (m *Monitor) UnmonitorSubreddits(subreddits ...string) {
	m.mu.Lock()
	setKeys(m.monitoredSubreddits, false, subreddits)
	m.subredditQuery = buildQuery(m.monitoredSubreddits, "+")
	m.mu.Unlock()
}

// MonitorThreads starts monitoring the requested threads.
func (m *Monitor) MonitorThreads(threads ...string) {
	m.mu.Lock()
	setKeys(m.monitoredThreads, true, threads)
	m.threadQuery = buildQuery(m.monitoredThreads, ",")
	m.mu.Unlock()
}

// UnmonitorThreads stops monitoring the requested threads.
func (m *Monitor) UnmonitorThreads(threads ...string) {
	m.mu.Lock()
	setKeys(m.monitoredThreads, false, threads)
	m.threadQuery = buildQuery(m.monitoredThreads, ",")
	m.mu.Unlock()
}

// fetchTip fetches the latest posts from the monitored subreddits.
func (m *Monitor) fetchTip() ([]*redditproto.Link, error) {
	posts, err := m.op.Scrape(
		m.subredditQuery,
		"new",
		"",
		m.tip.Front().Value.(string),
		maxPosts,
	)
	if err != nil {
		return nil, err
	}

	for i := range posts {
		m.tip.PushFront(posts[len(posts)-1-i].GetName())
		if m.tip.Len() > maxTipSize {
			m.tip.Remove(m.tip.Back())
		}
	}

	return posts, nil
}

// fixTip fixes the tip if the post has been deleted. fixTip returns whether
// the tip was broken.
func (m *Monitor) fixTip() (bool, error) {
	wasBroken := false
	ids := make([]string, m.tip.Len())
	for e := m.tip.Front(); e != nil; e = e.Next() {
		ids = append(ids, e.Value.(string))
	}
	posts, err := m.op.Threads(ids...)
	if err != nil {
		return false, err
	}

	for e := m.tip.Front(); e != nil; e = e.Next() {
		if e.Prev() != nil {
			wasBroken = true
			m.tip.Remove(e.Prev())
		}
		for _, post := range posts {
			if e.Value.(string) == post.GetName() {
				return wasBroken, nil
			}
		}
	}
	m.tip.Remove(m.tip.Front())
	m.tip.PushFront("")

	return wasBroken, nil
}

// setKeys sets the value of all provided keys to val in m.
func setKeys(m map[string]bool, val bool, keys []string) {
	for _, key := range keys {
		m[key] = val
	}
}

// buildQuery assembles a delimited list of some kind of name to use as a query
// to reddit, from a map indicating whether each name should be included in the
// query.
func buildQuery(names map[string]bool, delim string) string {
	var queryBuffer bytes.Buffer
	emptyQuery := true

	for name, include := range names {
		if include {
			emptyQuery = false
			queryBuffer.WriteString(name)
			queryBuffer.WriteString(delim)
		}
	}

	if emptyQuery {
		return ""
	}
	query := queryBuffer.String()

	return query[:len(query)-len(delim)]
}