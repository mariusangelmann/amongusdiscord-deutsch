package main

import (
	"errors"
	"io"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/denverquane/amongusdiscord/storage"

	"github.com/denverquane/amongusdiscord/discord"
	"github.com/joho/godotenv"
)

const VERSION = "2.3.2-Prerelease"

//TODO if running in shard mode, we don't want to use the default port. Each shard should prob run on their own port
const DefaultPort = "8123"
const DefaultURL = "http://localhost"

func main() {
	err := discordMainWrapper()
	if err != nil {
		log.Println("Programm mit folgendem Fehler beendet:")
		log.Println(err)
		log.Println("Dieses Fenster wird automatisch in 10 Sekunden beendet")
		time.Sleep(10 * time.Second)
		return
	}
}

func discordMainWrapper() error {
	err := godotenv.Load("config.txt")
	if err != nil {
		err = godotenv.Load("final.txt")
		if err != nil {
			log.Println("Konfigurationsdatei kann nicht geöffnet werden, hoffentlich läuft Programm im Docker und  DISCORD_BOT_TOKEN wurde bereitgestellt...")
			f, err := os.Create("config.txt")
			if err != nil {
				log.Println("Problem beim Erstellen der Beispielkonfiguration config.txt")
				return err
			}
			_, err = f.WriteString("DISCORD_BOT_TOKEN = \n")
			f.Close()
		}
	}

	logEntry := os.Getenv("DISABLE_LOG_FILE")
	if logEntry == "" {
		file, err := os.Create("logs.txt")
		if err != nil {
			return err
		}
		mw := io.MultiWriter(os.Stdout, file)
		log.SetOutput(mw)
	}

	emojiGuildID := os.Getenv("EMOJI_GUILD_ID")

	log.Println(VERSION)

	discordToken := os.Getenv("DISCORD_BOT_TOKEN")
	if discordToken == "" {
		return errors.New("kein DISCORD_BOT_TOKEN bereitgestellt")
	}

	discordToken2 := os.Getenv("DISCORD_BOT_TOKEN_2")
	if discordToken2 != "" {
		log.Println("Sie haben einen 2. Discord Bot Token bereitgestellt, daher werde ich versuchen, ihn zu verwenden")
	}

	numShardsStr := os.Getenv("NUM_SHARDS")
	numShards, err := strconv.Atoi(numShardsStr)
	if err != nil {
		numShards = 1
	}
	ports := make([]string, numShards)
	tempPort := strings.ReplaceAll(os.Getenv("PORT"), " ", "")
	portStrings := strings.Split(tempPort, ",")
	if len(ports) == 0 || len(tempPort) == 0 {
		num, err := strconv.Atoi(tempPort)

		if err != nil || num < 1024 || num > 65535 {
			log.Printf("[Info] Ungültiger oder kein bestimmter PORT (Bereich [1024-65535]) angegeben. Standardmäßig gesetzt auf %s\n", DefaultPort)
			ports[0] = DefaultPort
		}
	} else if len(portStrings) == numShards {
		for i := 0; i < numShards; i++ {
			num, err := strconv.Atoi(portStrings[i])
			if err != nil || num < 0 || num > 65535 {
				return errors.New("ungültiger oder kein bestimmter PORT (Bereich [0-65535]) provided")
			}
			ports[i] = portStrings[i]
		}
	} else {
		return errors.New("Die Anzahl der Shards stimmt nicht mit der Anzahl der bereitgestellten Ports überein")
	}

	url := os.Getenv("SERVER_URL")
	if url == "" {
		log.Printf("[Info] Keine gültige SERVER_URL angegeben. Standardmäßig gesetzt auf %s\n", DefaultURL)
		url = DefaultURL
	}

	extPort := os.Getenv("EXT_PORT")
	if extPort == "" {
		log.Print("[Info] Kein EXT_PORT bereitgestellt. Standardmäßig gesetzt auf PORT\n")
	} else if extPort == "protocol" {
		log.Print("[Info] EXT_PORT auf Protokoll gesetzt. Der URL wird kein Port hinzugefügt\n")
	} else {
		num, err := strconv.Atoi(extPort)
		if err != nil || num > 65535 || (num < 1024 && num != 80 && num != 443) {
			return errors.New("ungültiger EXT_PORT (Bereich [1024-65535]) bereitgestellt")
		}
	}

	var storageClient storage.StorageInterface
	dbSuccess := false

	authPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	projectID := os.Getenv("FIRESTORE_PROJECT_ID")
	if authPath != "" && projectID != "" {
		log.Println("Die Variable GOOGLE_APPLICATION_CREDENTIALS wird gesetzt. Versuch, Firestore als Speichertreiber zu verwenden")
		storageClient = &storage.FirestoreDriver{}
		err = storageClient.Init(projectID)
		if err != nil {
			log.Printf("Fehler beim Erstellen des Firestore-Clients mit Fehler: %s", err)
		} else {
			dbSuccess = true
			log.Println("Erfolgreiche Initialisierung des Firestore-Clients als Speichertreiber")
		}
	}

	if !dbSuccess {
		storageClient = &storage.FilesystemDriver{}
		configPath := os.Getenv("CONFIG_PATH")
		if configPath == "" {
			configPath = "./"
		}
		log.Printf("Verwenden von %s als Basispfad für die Konfiguration", configPath)
		err := storageClient.Init(configPath)
		if err != nil {
			log.Fatalf("Fehler beim Erstellen des Dateisystem-Speichertreibers mit Fehler: %s", err)
		}
		log.Println("Erfolgreiche Initialisierung des lokalen Dateisystems als Speichertreiber")
	}
	log.Println("Bot läuft jetzt. Drücke STRG-C, um den Vorgang zu beenden.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)

	bots := make([]*discord.Bot, numShards)

	for i := 0; i < numShards; i++ {
		bots[i] = discord.MakeAndStartBot(VERSION, discordToken, discordToken2, url, ports[i], extPort, emojiGuildID, numShards, i, storageClient)
	}

	go discord.MessagesServer("5000", bots)

	<-sc
	for i := 0; i < numShards; i++ {
		bots[i].Close()
	}
	storageClient.Close()
	return nil
}
