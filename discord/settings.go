package discord

import (
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/denverquane/amongusdiscord/game"
	"github.com/denverquane/amongusdiscord/storage"
	"log"
	"strconv"
	"strings"
)

func HandleSettingsCommand(s *discordgo.Session, m *discordgo.MessageCreate, guild *GuildState, storageInterface storage.StorageInterface, args []string) {
	// if no arg passed, send them list of possible settings to change
	if len(args) == 1 {
		s.ChannelMessageSend(m.ChannelID, "Die Liste der möglichen Einstellungen ist:\n"+
			"•`CommandPrefix [prefix]`: Ändere die Präfix des Bots auf diesem Server\n"+
			"•`DefaultTrackedChannel [voiceChannel]`: Ändere den standardmäßigen Sprachkanal, den der Bot verfolgt\n"+
			"•`AdminUserIDs [user 1] [user 2] [etc]`: Hinzufügen oder Entfernen von Bot-Administratoren und Benutzern, die dem Bot Befehle geben können\n"+
			"•`PermissionRoleIDs [role 1] [role 2] [etc]`: Hinzufügen oder Entfernen von Bot-Administratorrollen, also Rollen, die dem Bot Befehle geben können.\n"+
			"•`ApplyNicknames [true/false]`: Ob der Bot die Spitznamen der Spieler ändern soll, um die Farbe des Spielers wiederzugeben\n"+
			"•`UnmuteDeadDuringTasks [true/false]`: Ob der Bot tote Spieler sofort stumm schalten soll, wenn sie sterben (**WARNUNG**: enthüllt Informationen)\n"+
			"•`Delays [old game phase] [new game phase] [delay]`: Ändere die Verzögerung zwischen dem Ändern der Spielphase und dem Stummschalten/aufheben der Stummschaltung von Spielern\n"+
			"•`VoiceRules [mute/deaf] [game phase] [alive/dead] [true/false]`: Ob lebende/tote Spieler während dieser Spielphase stumm geschaltet/betäubt werden sollen")
		return
	}
	// if command invalid, no need to reapply changes to json file
	isValid := false
	switch args[1] {
	case "commandprefix":
		fallthrough
	case "prefix":
		fallthrough
	case "cp":
		isValid = CommandPrefixSetting(s, m, guild, args)
	case "defaulttrackedchannel":
		fallthrough
	case "channel":
		fallthrough
	case "vc":
		fallthrough
	case "dtc":
		isValid = SettingDefaultTrackedChannel(s, m, guild, args)
	case "adminuserids":
		fallthrough
	case "admins":
		fallthrough
	case "admin":
		fallthrough
	case "auid":
		fallthrough
	case "aui":
		fallthrough
	case "a":
		isValid = SettingAdminUserIDs(s, m, guild, args)
	case "permissionroleids":
		fallthrough
	case "roles":
		fallthrough
	case "role":
		fallthrough
	case "prid":
		fallthrough
	case "pri":
		fallthrough
	case "r":
		isValid = SettingPermissionRoleIDs(s, m, guild, args)
	case "applynicknames":
		fallthrough
	case "nicknames":
		fallthrough
	case "nickname":
		fallthrough
	case "an":
		isValid = SettingApplyNicknames(s, m, guild, args)
	case "unmutedeadduringtasks":
		fallthrough
	case "unmute":
		fallthrough
	case "uddt":
		isValid = SettingUnmuteDeadDuringTasks(s, m, guild, args)
	case "delays":
		fallthrough
	case "d":
		isValid = SettingDelays(s, m, guild, args)
	case "voicerules":
		fallthrough
	case "vr":
		isValid = SettingVoiceRules(s, m, guild, args)
	default:
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Sorry, `%s` ist keine gültige Einstellung!\n"+
			"Gültige Einstellungen sind `CommandPrefix`, `DefaultTrackedChannel`, `AdminUserIDs`, `ApplyNicknames`, `UnmuteDeadDuringTasks`, `Delays` und `VoiceRules`.", args[1]))
	}
	if isValid {
		data, err := guild.PersistentGuildData.ToData()
		if err != nil {
			log.Println(err)
		} else {
			err := storageInterface.WriteGuildData(m.GuildID, data)
			if err != nil {
				log.Println(err)
			}
		}
	}
}

func CommandPrefixSetting(s *discordgo.Session, m *discordgo.MessageCreate, guild *GuildState, args []string) bool {
	if len(args) == 2 {
		s.ChannelMessageSend(m.ChannelID, "`CommandPrefix [prefix]`: Ändere die Prefix des Bots auf diesem Server.")
		return false
	}
	if len(args[2]) > 10 {
		// prevent someone from setting something ridiculous lol
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Sorry, die Prefix `%s` es ist zu lang (%d Zeichen, max 10). Versuche etwas kürzeres.", args[2], len(args[2])))
		return false
	}
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Das Gildenpräfix wurde von `%s` zu `%s` geändert. Nutze das von jetzt an!",
		guild.PersistentGuildData.CommandPrefix, args[2]))
	guild.PersistentGuildData.CommandPrefix = args[2]
	return true
}

func SettingDefaultTrackedChannel(s *discordgo.Session, m *discordgo.MessageCreate, guild *GuildState, args []string) bool {
	if len(args) == 2 {
		// give them both command syntax and current voice channel
		channelList, _ := s.GuildChannels(m.GuildID)
		for _, c := range channelList {
			if c.ID == guild.PersistentGuildData.DefaultTrackedChannel {
				s.ChannelMessageSend(m.ChannelID, "`DefaultTrackedChannel [voiceChannel]`: Ändere den standardmäßigen Sprachkanal, den der Bot verfolgt.\n"+
					fmt.Sprintf("Derzeit verfolge ich den `%s` Sprachkanal", c.Name))
				return false
			}
		}
		s.ChannelMessageSend(m.ChannelID, "`DefaultTrackedChannel [voiceChannel]`: Ändere den standardmäßigen Sprachkanal, den der Bot verfolgt.\n"+
			"Derzeit verfolge ich keinen Sprachkanal. Entweder ist die ID ungültig oder du hast mir keine gegeben.")
		return false
	}
	// now to find the channel they are referencing
	channelID := ""
	channelName := "" // we track name to confirm to the user they selected the right channel
	channelList, _ := s.GuildChannels(m.GuildID)
	for _, c := range channelList {
		// Check if channel is a voice channel
		if c.Type != discordgo.ChannelTypeGuildVoice {
			continue
		}
		// check if this is the right channel
		if strings.ToLower(c.Name) == args[2] || c.ID == args[2] {
			channelID = c.ID
			channelName = c.Name
			break
		}
	}
	// check if channel was found
	if channelID == "" {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Der Sprachkanal `%s` konnte nicht gefunden werden! Geben den Namen oder die ID ein und stelle sicher, dass der Bot sie sehen kann.", args[2]))
		return false
	} else {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Der Standard-Sprachkanal wurde geändert zu `%s`. Verwende das von nun an!",
			channelName))
		guild.PersistentGuildData.DefaultTrackedChannel = channelID
		return true
	}
}

func SettingAdminUserIDs(s *discordgo.Session, m *discordgo.MessageCreate, guild *GuildState, args []string) bool {
	if len(args) == 2 {
		adminCount := len(guild.PersistentGuildData.AdminUserIDs) // caching for optimisation
		// make a nicely formatted string of all the admins: "user1, user2, user3 and user4"
		if adminCount == 0 {
			s.ChannelMessageSend(m.ChannelID, "`AdminUserIDs [user 1] [user 2] [etc]`: Hinzufügen oder Entfernen von Bot-Administratoren, also Benutzer, die dem Bot Befehle geben können.\n"+
				"Derzeit gibt es keine Bot-Administratoren.")
		} else if adminCount == 1 {
			s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
				Content: "`AdminUserIDs [user 1] [user 2] [etc]`: Hinzufügen oder Entfernen von Bot-Administratoren, also Benutzer, die dem Bot Befehle geben können.\n" +
					fmt.Sprintf("Derzeit ist der einzige Administrator <@%s>.", guild.PersistentGuildData.AdminUserIDs[0]),
				AllowedMentions: &discordgo.MessageAllowedMentions{Users: nil},
			})
		} else {
			listOfAdmins := ""
			for index, ID := range guild.PersistentGuildData.AdminUserIDs {
				if index == 0 {
					listOfAdmins += "<@" + ID + ">"
				} else if index == adminCount-1 {
					listOfAdmins += " and <@" + ID + ">"
				} else {
					listOfAdmins += ", <@" + ID + ">"
				}
			}
			// mention users without pinging
			s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
				Content: "`AdminUserIDs [user 1] [user 2] [etc]`: Hinzufügen oder Entfernen von Bot-Administratoren, also Benutzer, die dem Bot Befehle geben können.\n" +
					fmt.Sprintf("Derzeit sind die Admins %s.", listOfAdmins),
				AllowedMentions: &discordgo.MessageAllowedMentions{Users: nil},
			})
		}
		return false
	}
	// users the user mentioned in their message
	var userIDs []string

	for _, userName := range args[2:] {
		if userName == "" || userName == " " {
			// user added a double space by accident, ignore it
			continue
		}
		ID := getMemberFromString(s, m.GuildID, userName)
		if ID == "" {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Entschuldigung, ich weiß nicht wer `%s` ist. Du kannst mir seine/ihre ID, seinen/ihren Nutzername, username#XXXX, nickname geben oder ihn/sie @erwähnen", userName))
			continue
		}
		// check if id is already in array
		for _, IDinSlice := range userIDs {
			if ID == IDinSlice {
				// this user is mentioned more than once, ignore it
				ID = "already in list"
				break
			}
		}
		if ID != "already in list" {
			userIDs = append(userIDs, ID)
		}
	}

	// the index of admins to remove from AdminUserIDs
	var removeAdmins []int
	isValid := false

	for _, ID := range userIDs {
		// can't use guild.HasAdminPermissions() because we also need index
		for index, adminID := range guild.PersistentGuildData.AdminUserIDs {
			if ID == adminID {
				// add ID to IDs to be deleted
				removeAdmins = append(removeAdmins, index)
				ID = "" // indicate to other loop this ID has been dealt with
				break
			}
		}
		if ID != "" {
			guild.PersistentGuildData.AdminUserIDs = append(guild.PersistentGuildData.AdminUserIDs, ID)
			// mention user without pinging
			s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
				Content:         fmt.Sprintf("<@%s> ist jetzt ein Bot-Administrator!", ID),
				AllowedMentions: &discordgo.MessageAllowedMentions{Users: nil},
			})
			isValid = true
		}
	}

	if len(removeAdmins) == 0 {
		return isValid
	}

	// remove the values from guild.PersistentGuildData.AdminUserIDs by creating a
	// new array with only the admins the user didn't remove, and replacing the
	// current array with that one
	var newAdminList []string
	currentIndex := 0
	nextIndexToRemove := removeAdmins[0]
	currentIndexInRemoveAdmins := 0

	for currentIndex < len(guild.PersistentGuildData.AdminUserIDs) {
		if currentIndex != nextIndexToRemove {
			// user didn't remove this admin, add it to the list
			newAdminList = append(newAdminList, guild.PersistentGuildData.AdminUserIDs[currentIndex])
		} else {
			s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
				Content:         fmt.Sprintf("<@%s> ist kein Bot-Administrator mehr, RIP", guild.PersistentGuildData.AdminUserIDs[currentIndex]),
				AllowedMentions: &discordgo.MessageAllowedMentions{Users: nil},
			})
			currentIndexInRemoveAdmins++
			if currentIndexInRemoveAdmins < len(removeAdmins) {
				nextIndexToRemove = removeAdmins[currentIndexInRemoveAdmins]
			} else {
				// reached the end of removeAdmins
				newAdminList = append(newAdminList, guild.PersistentGuildData.AdminUserIDs[currentIndex+1:]...)
				break
			}
		}
		currentIndex++
	}

	guild.PersistentGuildData.AdminUserIDs = newAdminList
	return true
}

func SettingPermissionRoleIDs(s *discordgo.Session, m *discordgo.MessageCreate, guild *GuildState, args []string) bool {
	if len(args) == 2 {
		adminRoleCount := len(guild.PersistentGuildData.PermissionedRoleIDs) // caching for optimisation
		// make a nicely formatted string of all the roles: "role1, role2, role3 and role4"
		if adminRoleCount == 0 {
			s.ChannelMessageSend(m.ChannelID, "`PermissionRoleIDs [role 1] [role 2] [etc]`: Hinzufügen oder Entfernen von Bot-Administratorrollen, also Rollen, die dem Bot Befehle geben können.\n"+
				"Derzeit gibt es keine Bot-Administratorrollen.")
		} else if adminRoleCount == 1 {
			// mention role without pinging
			s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
				Content: "`PermissionRoleIDs [role 1] [role 2] [etc]`: Hinzufügen oder Entfernen von Bot-Administratorrollen, also Rollen, die dem Bot Befehle geben können.\n" +
					fmt.Sprintf("Derzeit ist die einzige Administratorrolle <&%s>.", guild.PersistentGuildData.PermissionedRoleIDs[0]),
				AllowedMentions: &discordgo.MessageAllowedMentions{Roles: nil},
			})
		} else {
			listOfRoles := ""
			for index, ID := range guild.PersistentGuildData.PermissionedRoleIDs {
				if index == 0 {
					listOfRoles += "<&" + ID + ">"
				} else if index == adminRoleCount-1 {
					listOfRoles += " and <&" + ID + ">"
				} else {
					listOfRoles += ", <&" + ID + ">"
				}
			}
			// mention roles without pinging
			s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
				Content: "`PermissionRoleIDs [role 1] [role 2] [etc]`: Hinzufügen oder Entfernen von Bot-Administratorrollen, also Rollen, die dem Bot Befehle geben können.\n" +
					fmt.Sprintf("Derzeit sind die Administratorrollen %s.", listOfRoles),
				AllowedMentions: &discordgo.MessageAllowedMentions{Roles: nil},
			})
		}
		return false
	}

	// roles the user mentioned in their message
	var roleIDs []string

	for _, roleName := range args[2:] {
		if roleName == "" || roleName == " " {
			// user added a double space by accident, ignore it
			continue
		}
		ID := getRoleFromString(s, m.GuildID, roleName)
		if ID == "" {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Sorry, Ich kenne die Rolle `%s` nicht. Du kannst mir die Rollen-ID, den Rollen-Namen geben oder die  @rolle erwähnen", roleName))
			continue
		}
		// check if id is already in array
		for _, IDinSlice := range roleIDs {
			if ID == IDinSlice {
				// this role is mentioned more than once, ignore it
				ID = "already in list"
				break
			}
		}
		if ID != "already in list" {
			roleIDs = append(roleIDs, ID)
		}
	}

	// index of roles to get adminated (or is it admin-ed...)
	var removeRoles []int
	isValid := false

	for _, ID := range roleIDs {
		// can't use guild.HasRolePermissions() because we also need index
		for index, adminRoleID := range guild.PersistentGuildData.PermissionedRoleIDs {
			if ID == adminRoleID {
				// add ID to IDs to be deleted
				removeRoles = append(removeRoles, index)
				ID = "" // indicate to other loop this ID has been dealt with
				break
			}
		}
		if ID != "" {
			guild.PersistentGuildData.PermissionedRoleIDs = append(guild.PersistentGuildData.PermissionedRoleIDs, ID)
			// mention user without pinging
			s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
				Content:         fmt.Sprintf("<@&%s>s sind jetzt Bot Admins!", ID),
				AllowedMentions: &discordgo.MessageAllowedMentions{Users: nil},
			})
			isValid = true
		}
	}

	if len(removeRoles) == 0 {
		return isValid
	}

	// same process as removeAdminIDs
	var newAdminRoleList []string
	currentIndex := 0
	nextIndexToRemove := removeRoles[0]
	currentIndexInRemoveAdminRoles := 0

	for currentIndex < len(guild.PersistentGuildData.PermissionedRoleIDs) {
		if currentIndex != nextIndexToRemove {
			// user didn't remove this role, add it to the list
			newAdminRoleList = append(newAdminRoleList, guild.PersistentGuildData.PermissionedRoleIDs[currentIndex])
		} else {
			s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
				Content:         fmt.Sprintf("<@&%s>s sind keine Bot-Admins mehr.", guild.PersistentGuildData.PermissionedRoleIDs[currentIndex]),
				AllowedMentions: &discordgo.MessageAllowedMentions{Users: nil},
			})
			currentIndexInRemoveAdminRoles++
			if currentIndexInRemoveAdminRoles < len(removeRoles) {
				nextIndexToRemove = removeRoles[currentIndexInRemoveAdminRoles]
			} else {
				// reached the end of removeRoles
				newAdminRoleList = append(newAdminRoleList, guild.PersistentGuildData.PermissionedRoleIDs[currentIndex+1:]...)
				break
			}
		}
		currentIndex++
	}

	guild.PersistentGuildData.PermissionedRoleIDs = newAdminRoleList
	return true
}

func SettingApplyNicknames(s *discordgo.Session, m *discordgo.MessageCreate, guild *GuildState, args []string) bool {
	if len(args) == 2 {
		if guild.PersistentGuildData.ApplyNicknames {
			s.ChannelMessageSend(m.ChannelID, "`ApplyNicknames [true/false]`: Ob der Bot die Spitznamen der Spieler ändern soll, um die Farbe des Spielers wiederzugeben.\n"+
				"Derzeit ändert der Bot Spitznamen.")
		} else {
			s.ChannelMessageSend(m.ChannelID, "`ApplyNicknames [true/false]`: Ob der Bot die Spitznamen der Spieler ändern soll, um die Farbe des Spielers wiederzugeben.\n"+
				"Derzeit ändert der Bot die Spitznamen **nicht**.")
		}
		return false
	}
	if args[2] == "true" {
		if guild.PersistentGuildData.ApplyNicknames {
			s.ChannelMessageSend(m.ChannelID, "Es ist bereits auf true gestellt")
		} else {
			s.ChannelMessageSend(m.ChannelID, "Ich werde jetzt die Spieler im Voice-Chat umbenennen.")
			guild.PersistentGuildData.ApplyNicknames = true
			return true
		}
	} else if args[2] == "false" {
		if guild.PersistentGuildData.ApplyNicknames {
			s.ChannelMessageSend(m.ChannelID, "Ich werde die Spieler im Voice-Chat nicht mehr umbenennen.")
			guild.PersistentGuildData.ApplyNicknames = false
			return true
		} else {
			s.ChannelMessageSend(m.ChannelID, "Es ist bereits auf false gestellt")
		}
	} else {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Sorry, `%s` ist weder `true` noch `false`.", args[2]))
	}
	return false
}

func SettingUnmuteDeadDuringTasks(s *discordgo.Session, m *discordgo.MessageCreate, guild *GuildState, args []string) bool {
	if len(args) == 2 {
		if guild.PersistentGuildData.UnmuteDeadDuringTasks {
			s.ChannelMessageSend(m.ChannelID, "`UnmuteDeadDuringTasks [true/false]`: Ob der Bot tote Spieler sofort stumm schalten soll, wenn sie sterben. "+
				"**WARNUNG**: enthüllt, wer gestorben ist, bevor die Diskussion beginnt! Benutzung auf eigene Gefahr.\n"+
				"Derzeit hebt der Bot die Stummschaltung der Spieler unmittelbar nach dem Tod auf.")
		} else {
			s.ChannelMessageSend(m.ChannelID, "`UnmuteDeadDuringTasks [true/false]`: Ob der Bot tote Spieler sofort stumm schalten soll, wenn sie sterben. "+
				"**WARNUNG**: enthüllt, wer gestorben ist, bevor die Diskussion beginnt! Benutzung auf eigene Gefahr.\n"+
				"Derzeit macht der Bot die Spieler **nicht** sofort nach dem Tod stumm.")
		}
		return false
	}
	if args[2] == "true" {
		if guild.PersistentGuildData.UnmuteDeadDuringTasks {
			s.ChannelMessageSend(m.ChannelID, "Es ist bereits auf true gestellt!")
		} else {
			s.ChannelMessageSend(m.ChannelID, "Ich werde jetzt die Toten sofort nach ihrem Tod stumm schalten. Vorsicht, dies zeigt, wer während des Spiels gestorben ist!")
			guild.PersistentGuildData.UnmuteDeadDuringTasks = true
			return true
		}
	} else if args[2] == "false" {
		if guild.PersistentGuildData.UnmuteDeadDuringTasks {
			s.ChannelMessageSend(m.ChannelID, "Ich werde nicht länger sofort die Stummschaltung von Toten aufheben. Gute Wahl!")
			guild.PersistentGuildData.UnmuteDeadDuringTasks = false
			return true
		} else {
			s.ChannelMessageSend(m.ChannelID, "Es ist bereits auf false gestellt!")
		}
	} else {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Sorry, `%s` ist weder `true` noch `false`.", args[2]))
	}
	return false
}

func SettingDelays(s *discordgo.Session, m *discordgo.MessageCreate, guild *GuildState, args []string) bool {
	if len(args) == 2 {
		s.ChannelMessageSend(m.ChannelID, "`Delays [old game phase] [new game phase] [delay]`: CÄndern Sie die Verzögerung zwischen dem Ändern der Spielphase und dem Stummschalten / Aufheben der Stummschaltung von Spielern.")
		return false
	}
	// user passes phase name, phase name and new delay value
	if len(args) < 4 {
		// user didn't pass 2 phases, tell them the list of game phases
		s.ChannelMessageSend(m.ChannelID, "Die Liste der Spielphasen ist `Lobby`, `Tasks` und `Discussion`.\n"+
			"Du musst beide Phasen eingeben, von denen das Spiel wechselt, und die Verzögerung ändern.") // find a better wording for this at some point
		return false
	}
	// now to find the actual game state from the string they passed
	var gamePhase1 = getPhaseFromString(args[2])
	var gamePhase2 = getPhaseFromString(args[3])
	if gamePhase1 == game.UNINITIALIZED {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Ich weiß nicht was `%s` ist. Die Liste der Spielphasen ist `Lobby`, `Tasks` und `Discussion`.", args[2]))
		return false
	} else if gamePhase2 == game.UNINITIALIZED {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Ich weiß nicht was `%s` ist. Die Liste der Spielphasen ist `Lobby`, `Tasks` und `Discussion`.", args[3]))
		return false
	}
	oldDelay := guild.PersistentGuildData.Delays.GetDelay(gamePhase1, gamePhase2)
	if len(args) == 4 {
		// no number was passed, user was querying the delay
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Derzeit ist die Verzögerung beim Übergeben von `%s` zu `%s` ist %d.", args[2], args[3], oldDelay))
		return false
	}
	newDelay, err := strconv.Atoi(args[4])
	if err != nil || newDelay < 0 {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("`%s` ist keine gültige Nummer! Bitte versuche es erneut", args[4]))
		return false
	}
	guild.PersistentGuildData.Delays.Delays[game.PhaseNames[gamePhase1]][game.PhaseNames[gamePhase2]] = newDelay
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Die Verzögerung beim Übergeben von `%s` zu `%s` wurde gewechselt von %d zu %d.", args[2], args[3], oldDelay, newDelay))
	return true
}

func SettingVoiceRules(s *discordgo.Session, m *discordgo.MessageCreate, guild *GuildState, args []string) bool {
	if len(args) == 2 {
		s.ChannelMessageSend(m.ChannelID, "`VoiceRules [mute/deaf] [game phase] [alive/dead] [true/false]`: Ob lebende / tote Spieler während dieser Spielphase stumm geschaltet / betäubt werden sollen.")
		return false
	}
	// now for a bunch of input checking
	if len(args) < 5 {
		// user didn't pass enough args
		s.ChannelMessageSend(m.ChannelID, "Du hast nicht genug Argumente angegeben! Richtige Syntax ist: `VoiceRules [mute/deaf] [game phase] [alive/dead] [true/false]`")
		return false
	}
	if args[2] == "deaf" {
		args[2] = "deafened" // for formatting later on
	} else if args[2] == "mute" {
		args[2] = "muted" // same here
	} else {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("`%s` ist weder `mute` noch `deaf`!", args[2]))
		return false
	}
	gamePhase := getPhaseFromString(args[3])
	if gamePhase == game.UNINITIALIZED {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Ich weiß nicht was %s ist. Die Liste der Spielphasen ist `Lobby`, `Tasks` und `Discussion`.", args[3]))
		return false
	}
	if args[4] != "alive" && args[4] != "dead" {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("`%s` ist weder `alive` oder `dead`!", args[4]))
		return false
	}
	var oldValue bool
	if args[2] == "muted" {
		oldValue = guild.PersistentGuildData.VoiceRules.MuteRules[game.PhaseNames[gamePhase]][args[4]]
	} else {
		oldValue = guild.PersistentGuildData.VoiceRules.DeafRules[game.PhaseNames[gamePhase]][args[4]]
	}
	if len(args) == 5 {
		// user was only querying
		if oldValue {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Wenn in `%s` Phase, dann sind %s derzeit %s.", args[3], args[4], args[2]))
		} else {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Wenn in `%s` Phase, dann sind %s Spieler NICHT %s.", args[3], args[4], args[2]))
		}
		return false
	}
	var newValue bool
	if args[5] == "true" {
		newValue = true
	} else if args[5] == "false" {
		newValue = false
	} else {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("`%s` ist weder `true` oder `false`!", args[5]))
		return false
	}
	if newValue == oldValue {
		if newValue {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Wenn in `%s` Phase, dann sind %s Spieler bereits %s!", args[3], args[4], args[2]))
		} else {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Wenn in `%s` Phase, dann sind %s Spieler bereits un%s!", args[3], args[4], args[2]))
		}
		return false
	}
	if args[2] == "muted" {
		guild.PersistentGuildData.VoiceRules.MuteRules[game.PhaseNames[gamePhase]][args[4]] = newValue
	} else {
		guild.PersistentGuildData.VoiceRules.DeafRules[game.PhaseNames[gamePhase]][args[4]] = newValue
	}
	if newValue {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Von nun an, wenn in `%s` Phase, %s Spieler werden %s sein.", args[3], args[4], args[2]))
	} else {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Von nun an, wenn in `%s` Phase, %s Spieler werden un%s sein.", args[3], args[4], args[2]))
	}
	return true
}
