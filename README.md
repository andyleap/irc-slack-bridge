# irc-slack-bridge+

To use, simply create a file named `config.json` containing the following json configuration data:

```
{
	"Server":"<irc server to connect to>",
	"Channel": "#<ircchannel>",
	"Nick":"<irc nickname for slack bridge user>",
	"Suffix": "<suffix to attach to slack user names in irc>",
	"BlacklistUsers":["slackbot"],
	"SlackAPI": "<slack bot user integration token>",
	"SlackChannel": "<slackchannel>",
	"SlackIcon": "https://robohash.org/$username.png?size=48x48"
}
```

You may add additional users to the blacklist, if you so desire, and choose an alternate slack avatar icon source.

Then simply run the bridge.  It will connect to both slack and irc, and begin bridging chat back and forth.  Users on the irc side will automatically be connected/disconnected to match presence in the slack channel.
