package command

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Route holds information about a specific message route handler
type Route struct {
	Pattern        string      // match pattern that should trigger this route handler
	Aliases        []string    // pattern aliases
	Description    string      // short description of this route
	Help           string      // detailed help string for this route
	Run            HandlerFunc // route handler function to call
	JoinedPatterns string
}

// Context holds a bit of extra data we pass along to route handlers
// This way processing some of this only needs to happen once.
type Context struct {
	Fields          []string
	Content         string
	IsDirected      bool
	IsPrivate       bool
	HasPrefix       bool
	HasMention      bool
	HasMentionFirst bool
}

// HandlerFunc is the function signature required for a message route handler.
type HandlerFunc func(*discordgo.Session, *discordgo.Message, *Context)

// Mux is the main struct for all mux methods.
type Mux struct {
	Routes  []*Route
	Default *Route
	Prefix  string
}

// New returns a new Discord message route mux
func New() *Mux {
	m := &Mux{}
	return m
}

// Route allows you to register a route
func (m *Mux) Route(pattern, desc string, cb HandlerFunc, aliases ...string) (*Route, error) {

	r := Route{}
	r.Pattern = pattern
	r.Aliases = aliases
	r.Description = desc
	r.Run = cb
	r.JoinedPatterns = strings.Join(append([]string{r.Pattern}, r.Aliases...), "|")
	m.Routes = append(m.Routes, &r)

	return &r, nil
}

// FindRoute attempts to find the best route match for a given message.
func (m *Mux) FindRoute(msg string) *Route {

	// Tokenize the msg string into a slice of words
	fields := strings.Fields(msg)

	// no point to continue if there's no fields
	if len(fields) == 0 {
		return nil
	}

	toFind := fields[0]

	// Search though the command list for a match
	var r *Route

	for _, rv := range m.Routes {

		patterns := append([]string{rv.Pattern}, rv.Aliases...)

		for _, pattern := range patterns {
			if pattern == toFind {
				return rv
			}
		}
	}

	return r
}

// OnMessageCreate is a DiscordGo Event Handler function.  This must be
// registered using the DiscordGo.Session.AddHandler function.  This function
// will receive all Discord messages and parse them for matches to registered
// routes.
func (m *Mux) OnMessageCreate(ds *discordgo.Session, mc *discordgo.MessageCreate) {

	var err error

	// Ignore all messages created by the Bot account itself and other bots
	if mc.Author.ID == ds.State.User.ID || mc.Author.Bot {
		return
	}

	// Create Context struct that we can put various infos into
	ctx := &Context{
		Content: strings.TrimSpace(mc.Content),
		Fields:  strings.Fields(strings.TrimSpace(mc.Content)),
	}

	// Fetch the channel for this Message
	var c *discordgo.Channel
	c, err = ds.State.Channel(mc.ChannelID)
	if err != nil {
		// Try fetching via REST API
		c, err = ds.Channel(mc.ChannelID)
		if err != nil {
			log.Printf("unable to fetch Channel for Message, %s", err)
		} else {
			// Attempt to add this channel into our State
			err = ds.State.ChannelAdd(c)
			if err != nil {
				log.Printf("error updating State with Channel, %s", err)
			}
		}
	}
	// Add Channel info into Context (if we successfully got the channel)
	if c != nil {
		if c.Type == discordgo.ChannelTypeDM {
			ctx.IsPrivate, ctx.IsDirected = true, true
		}
	}

	// Detect @name or @nick mentions
	if !ctx.IsDirected {

		// Detect if Bot was @mentioned
		for _, v := range mc.Mentions {

			if v.ID == ds.State.User.ID {

				ctx.IsDirected, ctx.HasMention = true, true

				reg := regexp.MustCompile(fmt.Sprintf("<@!?(%s)>", ds.State.User.ID))

				// Was the @mention the first part of the string?
				if reg.FindStringIndex(ctx.Content)[0] == 0 {
					ctx.HasMentionFirst = true
				}

				// strip bot mention tags from content string
				ctx.Content = reg.ReplaceAllString(ctx.Content, "")

				break
			}
		}
	}

	// Detect prefix mention
	if !ctx.IsDirected && len(m.Prefix) > 0 {

		// TODO : Must be changed to support a per-guild user defined prefix
		if strings.HasPrefix(ctx.Content, m.Prefix) {
			ctx.IsDirected, ctx.HasPrefix, ctx.HasMentionFirst = true, true, true
			ctx.Content = strings.TrimPrefix(ctx.Content, m.Prefix)
		}
	}

	// For now, if we're not specifically mentioned we do nothing.
	// later I might add an option for global non-mentioned command words
	if !ctx.IsDirected {
		return
	}

	// Try to find the "best match" command out of the message.
	r := m.FindRoute(ctx.Content)
	if r != nil {
		r.Run(ds, mc.Message, ctx)
		return
	}

	// If no command match was found, call the default.
	// Ignore if only @mentioned in the middle of a message
	if m.Default != nil && (ctx.HasMentionFirst) {
		// TODO: This could use a ratelimit
		// or should the ratelimit be inside the cmd handler?..
		// In the case of "talking" to another bot, this can create an endless
		// loop.  Probably most common in private messages.
		m.Default.Run(ds, mc.Message, ctx)
	}

}
