package discord

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/denverquane/amongusdiscord/game"
)

func helpResponse(version, CommandPrefix string) string {
	buf := bytes.NewBuffer([]byte{})
	buf.WriteString(fmt.Sprintf("Among Us Bot Commands (v%s):\n", version))
	buf.WriteString("Haben Sie Probleme oder Vorschläge? Trete dem Discord-Server des originalen Entwickers bei (Englisch) <https://discord.gg/ZkqZSWF>!\n")
	buf.WriteString(fmt.Sprintf("`%s help` oder `%s h`: Hilfeinformationen und Befehlsverwendung anzeigen.\n", CommandPrefix, CommandPrefix))
	buf.WriteString(fmt.Sprintf("`%s new` oder `%s n`: Starten Sie das Spiel in diesem Textkanal. Akzeptiert Raumcode und Region als Argumente. z.B.: `%s new CODE eu`. Funktioniert auch zum Neustart.\n", CommandPrefix, CommandPrefix, CommandPrefix))
	buf.WriteString(fmt.Sprintf("`%s refresh` oder `%s r`: Erstelle die Statusmeldung des Bots vollständig neu, falls sie zu weit oben im Chat landet.\n", CommandPrefix, CommandPrefix))
	buf.WriteString(fmt.Sprintf("`%s end` oder `%s e`: Beende das Spiel vollständig und höre auf, Spieler zu verfolgen. Hebt die Stummschaltung auf und setzt den Status zurück.\n", CommandPrefix, CommandPrefix))
	buf.WriteString(fmt.Sprintf("`%s track` oder `%s t`: Weise den Bot an, nur den bereitgestellten Sprachkanal für die Automatisierung zu verwenden. z.B.: `%s t <vc_name>`\n", CommandPrefix, CommandPrefix, CommandPrefix))
	buf.WriteString(fmt.Sprintf("`%s link` oder `%s l`: Verknüpfe einen Spieler manuell mit seinem Namen oder seiner Farbe im Spiel. z.B.: `%s l @player cyan` oder `%s l @player bob`\n", CommandPrefix, CommandPrefix, CommandPrefix, CommandPrefix))
	buf.WriteString(fmt.Sprintf("`%s unlink` oder `%s u`: Löse manuell die Verknüpfung eines Spielers. z.B.: `%s u @player`\n", CommandPrefix, CommandPrefix, CommandPrefix))
	buf.WriteString(fmt.Sprintf("`%s settings` oder `%s s`: Anzeigen und Ändern von Einstellungen für den Bot, z.B. das Befehlspräfix oder das Stummschaltungsverhalten\n", CommandPrefix, CommandPrefix))
	buf.WriteString(fmt.Sprintf("`%s force` oder `%s f`: Erzwinge einen Übergang zu einer Stufe, wenn der Status fehlerhaft ist. z.B.: `%s f task` or `%s f d`(discuss)\n", CommandPrefix, CommandPrefix, CommandPrefix, CommandPrefix))

	return buf.String()
}

func (guild *GuildState) trackChannelResponse(channelName string, allChannels []*discordgo.Channel, forGhosts bool) string {
	for _, c := range allChannels {
		if (strings.ToLower(c.Name) == strings.ToLower(channelName) || c.ID == channelName) && c.Type == 2 {

			guild.Tracking.AddTrackedChannel(c.ID, c.Name, forGhosts)

			log.Println(fmt.Sprintf("Verfolge jetzt \"%s\" Voice Channel für Automute (für Geister? %v)!", c.Name, forGhosts))
			return fmt.Sprintf("Verfolge jetzt \"%s\" Voice Channel für Automute (für Geister? %v)", c.Name, forGhosts)
		}
	}
	return fmt.Sprintf("Kein Kanal mit dem Namen gefunden: %s!\n", channelName)
}

func (guild *GuildState) linkPlayerResponse(s *discordgo.Session, GuildID string, args []string) {

	g, err := s.State.Guild(guild.PersistentGuildData.GuildID)
	if err != nil {
		log.Println(err)
		return
	}

	userID := getMemberFromString(s, GuildID, args[0])
	if userID == "" {
		log.Printf("Sorry, ich weiß nicht, wer `%s` ist. Du kannst die ID, den Nutzernamen, username#XXXX, den Nickanem eingeben oder @erwähnen", args[0])
	}

	_, added := guild.checkCacheAndAddUser(g, s, userID)
	if !added {
		log.Println("Keine Nutzer im Discord gefunden mit userID " + userID)
	}

	combinedArgs := strings.ToLower(strings.Join(args[1:], ""))

	if game.IsColorString(combinedArgs) {
		playerData := guild.AmongUsData.GetByColor(combinedArgs)
		if playerData != nil {
			found := guild.UserData.UpdatePlayerData(userID, playerData)
			if found {
				log.Printf("%s wurde erfolgreich mit einer Farbe verknüpft\n", userID)
			} else {
				log.Printf("Es wurde kein Spieler mit der ID %s gefunden\n", userID)
			}
		}
		return
	} else {
		playerData := guild.AmongUsData.GetByName(combinedArgs)
		if playerData != nil {
			found := guild.UserData.UpdatePlayerData(userID, playerData)
			if found {
				log.Printf("%s erfolgreich mit Namen verknüpft\n", userID)
			} else {
				log.Printf("Es wurde kein Spieler gefunden mit ID %s\n", userID)
			}
		}
	}
}

// TODO:
func gameStateResponse(guild *GuildState) *discordgo.MessageEmbed {
	// we need to generate the messages based on the state of the game
	messages := map[game.Phase]func(guild *GuildState) *discordgo.MessageEmbed{
		game.MENU:    menuMessage,
		game.LOBBY:   lobbyMessage,
		game.TASKS:   gamePlayMessage,
		game.DISCUSS: gamePlayMessage,
	}
	return messages[guild.AmongUsData.GetPhase()](guild)
}

func lobbyMetaEmbedFields(tracking *Tracking, room, region string, playerCount int, linkedPlayers int) []*discordgo.MessageEmbedField {
	str := tracking.ToStatusString()
	gameInfoFields := make([]*discordgo.MessageEmbedField, 4)
	gameInfoFields[0] = &discordgo.MessageEmbedField{
		Name:   "Room Code",
		Value:  fmt.Sprintf("%s", room),
		Inline: true,
	}
	gameInfoFields[1] = &discordgo.MessageEmbedField{
		Name:   "Region",
		Value:  fmt.Sprintf("%s", region),
		Inline: true,
	}
	gameInfoFields[2] = &discordgo.MessageEmbedField{
		Name:   "Verfolgung",
		Value:  str,
		Inline: true,
	}
	gameInfoFields[3] = &discordgo.MessageEmbedField{
		Name:   "Spieler verbunden",
		Value:  fmt.Sprintf("%v/%v", linkedPlayers, playerCount),
		Inline: false,
	}

	return gameInfoFields
}

// Thumbnail for the bot
var Thumbnail = discordgo.MessageEmbedThumbnail{
	URL:      "https://github.com/denverquane/amongusdiscord/blob/master/assets/botProfilePicture.jpg?raw=true",
	ProxyURL: "",
	Width:    200,
	Height:   200,
}

func menuMessage(g *GuildState) *discordgo.MessageEmbed {
	alarmFormatted := ":x:"
	if v, ok := g.SpecialEmojis["alarm"]; ok {
		alarmFormatted = v.FormatForInline()
	}
	color := 15158332 //red
	desc := ""
	if g.Linked {
		desc = g.makeDescription()
		color = 3066993
	} else {
		desc = fmt.Sprintf("%s**Kein Capture verlinkt! Klicke auf den Link in den DMs, um eine Verbindung herzustellen!**%s", alarmFormatted, alarmFormatted)
	}

	msg := discordgo.MessageEmbed{
		URL:         "",
		Type:        "",
		Title:       "Main Menu",
		Description: desc,
		Timestamp:   "",
		Footer:      nil,
		Color:       color,
		Image:       nil,
		Thumbnail:   nil,
		Video:       nil,
		Provider:    nil,
		Author:      nil,
		Fields:      nil,
	}
	return &msg
}

func lobbyMessage(g *GuildState) *discordgo.MessageEmbed {
	//gameInfoFields[2] = &discordgo.MessageEmbedField{
	//	Name:   "\u200B",
	//	Value:  "\u200B",
	//	Inline: false,
	//}
	room, region := g.AmongUsData.GetRoomRegion()
	gameInfoFields := lobbyMetaEmbedFields(&g.Tracking, room, region, g.AmongUsData.NumDetectedPlayers(), g.UserData.GetCountLinked())

	listResp := g.UserData.ToEmojiEmbedFields(g.AmongUsData.NameColorMappings(), g.AmongUsData.NameAliveMappings(), g.StatusEmojis)
	listResp = append(gameInfoFields, listResp...)

	alarmFormatted := ":x:"
	if v, ok := g.SpecialEmojis["alarm"]; ok {
		alarmFormatted = v.FormatForInline()
	}
	color := 15158332 //red
	desc := ""
	if g.Linked {
		desc = g.makeDescription()
		color = 3066993
	} else {
		desc = fmt.Sprintf("%s**Kein Capture verlinkt! Klicke auf den Link in den DMs, um eine Verbindung herzustellen!**%s", alarmFormatted, alarmFormatted)
	}

	msg := discordgo.MessageEmbed{
		URL:         "",
		Type:        "",
		Title:       "Lobby",
		Description: desc,
		Timestamp:   "",
		Footer: &discordgo.MessageEmbedFooter{
			Text:         "Reagiere auf diese Nachricht mit deiner Farbe im Spiel! (oder ❌ um zu verlassen)",
			IconURL:      "",
			ProxyIconURL: "",
		},
		Color:     color,
		Image:     nil,
		Thumbnail: nil,
		Video:     nil,
		Provider:  nil,
		Author:    nil,
		Fields:    listResp,
	}
	return &msg
}

func gamePlayMessage(guild *GuildState) *discordgo.MessageEmbed {
	// add the player list
	//guild.UserDataLock.Lock()
	room, region := guild.AmongUsData.GetRoomRegion()
	gameInfoFields := lobbyMetaEmbedFields(&guild.Tracking, room, region, guild.AmongUsData.NumDetectedPlayers(), guild.UserData.GetCountLinked())
	listResp := guild.UserData.ToEmojiEmbedFields(guild.AmongUsData.NameColorMappings(), guild.AmongUsData.NameAliveMappings(), guild.StatusEmojis)
	listResp = append(gameInfoFields, listResp...)
	//guild.UserDataLock.Unlock()
	var color int

	phase := guild.AmongUsData.GetPhase()

	switch phase {
	case game.TASKS:
		color = 3447003 //BLUE
	case game.DISCUSS:
		color = 10181046 //PURPLE
	default:
		color = 15158332 //RED
	}

	msg := discordgo.MessageEmbed{
		URL:         "",
		Type:        "",
		Title:       string(phase.ToString()),
		Description: guild.makeDescription(),
		Timestamp:   "",
		Color:       color,
		Footer:      nil,
		Image:       nil,
		Thumbnail:   nil,
		Video:       nil,
		Provider:    nil,
		Author:      nil,
		Fields:      listResp,
	}

	return &msg
}

func (guild *GuildState) makeDescription() string {
	buf := bytes.NewBuffer([]byte{})
	if !guild.GameRunning {
		buf.WriteString("\n**Bot ist angehalten! Stoppe die Pause mit `" + guild.PersistentGuildData.CommandPrefix + " p`!**\n\n")
	}

	author := guild.GameStateMsg.leaderID
	if author != "" {
		buf.WriteString("<@" + author + "> führt ein Among Us Spiel aus!\nTDas Spiel findet statt in ")
	}

	if len(guild.Tracking.tracking) == 0 {
		buf.WriteString("jedem Sprachkanal!")
	} else {
		t, err := guild.Tracking.FindAnyTrackedChannel(false)
		if err != nil {
			buf.WriteString("einem ungültiger Sprachkanal!")
		} else {
			buf.WriteString("dem **" + t.channelName + "** Sprachkanal!")
		}
	}

	return buf.String()
}

func extractUserIDFromMention(mention string) (string, error) {
	//nickname format
	if strings.HasPrefix(mention, "<@!") && strings.HasSuffix(mention, ">") {
		return mention[3 : len(mention)-1], nil
		//non-nickname format
	} else if strings.HasPrefix(mention, "<@") && strings.HasSuffix(mention, ">") {
		return mention[2 : len(mention)-1], nil
	} else {
		return "", errors.New("Erwähnung entspricht nicht dem richtigen Format")
	}
}

func extractRoleIDFromMention(mention string) (string, error) {
	//role is formatted <&123456>
	if strings.HasPrefix(mention, "<@&") && strings.HasSuffix(mention, ">") {
		return mention[3 : len(mention)-1], nil
	} else {
		return "", errors.New("Erwähnung entspricht nicht dem richtigen Format")
	}
}
