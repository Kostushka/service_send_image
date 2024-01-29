package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Kostushka/share-images/internal/db"
	"github.com/Kostushka/share-images/internal/web"
	"log"
	"os"
	"strconv"
	"syscall"
)

const serviceName = "share-image"

func main() {
	// получить конфигурационные данные
	conf, err := configParse()
	if err != nil {
		log.Fatalf("cannot get config data: %v", err)
	}

	log.Printf("Received command-line arguments: port %q; a directory for images %q; "+
		"a file with a form %q; URI for database %q; database name %q; collection name %q",
		conf.port, conf.imgDir, conf.formFile, conf.URIDb, conf.nameDb, conf.nameCollection)

	// определили бд с коллекцией
	db, err := db.NewDB(conf.URIDb, conf.nameDb, conf.nameCollection, conf.authData)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("db %s is defined", conf.nameDb)

	// объявили экземпляр структуры с данными формы, каталога для картинок, бд
	webServer, err := web.NewWeb(conf.formFile, conf.imgDir, db)
	if err != nil {
		log.Fatalf("cannot init webServer: %v", err)
	}

	// запуск слушателя и обработчика клиентских запросов
	if err := webServer.Run(conf.port); err != nil {
		log.Fatal(err)
	}
}

type config struct {
	port           string
	imgDir         string
	formFile       string
	URIDb          string
	nameDb         string
	nameCollection string
	authData       *db.Auth
}

func configParse() (*config, error) {

	var conf config

	// флаг порта, на котором будет слушать запущенный сервер
	var port int
	flag.IntVar(&port, "port", 80, "port for listen")

	// флаг каталога для изображений
	flag.StringVar(&conf.imgDir, "images-dir", "./images", "catalog for images")

	// флаг файла с формой
	flag.StringVar(&conf.formFile, "form-file", "", "form file")

	// адрес для запуска процесса работы с бд
	flag.StringVar(&conf.URIDb, "URI-db", "mongodb://localhost:27017", "URI for database")

	// название бд
	flag.StringVar(&conf.nameDb, "name-db", serviceName, "database name")

	// название коллекции в бд
	flag.StringVar(&conf.nameCollection, "name-collection", "images", "collection name")

	var authFilename string

	// название файла с данными аутентификации
	flag.StringVar(&authFilename, "auth-file", "", "auth file")

	flag.Parse()

	// распарсить файл с данными для аутентификации
	data, err := parseAuthfile(authFilename)
	if err != nil {
		return nil, err
	}
	conf.authData = data

	// порт должен быть корректным
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("invalid port value: %v", port)
	}
	conf.port = strconv.Itoa(port)

	// файл с формой должен быть указан в аргументах командной строки при запуске сервера
	if conf.formFile == "" {
		return nil, fmt.Errorf("There is no html file with the form in the command line args")
	}

	return &conf, nil
}

func parseAuthfile(filename string) (*db.Auth, error) {

	// получить информацию о файле
	info, err := os.Stat(filename)
	if err != nil {
		log.Printf("cannot get info about %v: %v", filename, err)
		return nil, err
	}
	// получить id владельца файл
	uidFile := info.Sys().(*syscall.Stat_t).Uid

	// получить id пользователя, запустившего текущий процесс
	uidCurrent := os.Getuid()

	// id пользователя, запустившего процесс, и id владельца должны совпадать
	if uidCurrent != int(uidFile) {
		return nil, fmt.Errorf("The user ID of the process does not match the ID of the owner of the file with the auth data")
	}

	// получить права доступа файла
	perm := fmt.Sprintf("%b", info.Mode().Perm())

	// преобразовать строку с правами доступа в двоичное числовое значение
	permb, err := strconv.ParseInt(perm, 2, 64)
	if err != nil {
		log.Printf("cannot interprets a string with perm and returns the corresponding int value: %v", err)
		return nil, err
	}

	// маска битов для проверки прав длы группы и всех пользователей
	flagCheck := 0b111111

	// если процесс запущен от root, ОС игнорирует маски прав доступа к файлу (пользователь сможет прочитать свой файл, даже если файл имеет права 0о000)
	// если процесс запущен от любого другого пользователя, у него должен быть доступ к чтению своего файла (1**), иначе доступ будет закрыт самой ОС
	// нам важно проверить, что к файлу нет доступа у групп и всех остальных пользователей
	if permb&int64(flagCheck) > 0 {
		log.Printf("Groups and others should not have access rights to the auth file: %v", filename)
		return nil, err
	}

	authFields := db.Auth{}

	// прочитать файл в буфер
	buf, err := os.ReadFile(filename)
	if err != nil {
		log.Printf("cannot read auth file: %v: %v", filename, err)
		return nil, err
	}

	// записать данные из буфера в структуру
	err = json.Unmarshal(buf, &authFields)
	if err != nil {
		log.Printf("cannot unmarshal buf with auth data: %v", err)
		return nil, err
	}

	return &authFields, nil
}
