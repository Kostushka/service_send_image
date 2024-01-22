package web

import (
	"bytes"
	"context"
	"fmt"
	"github.com/Kostushka/share-images/internal/db"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path"
	"syscall"
	"text/template"
	"time"
)

const maxUploadSize = 32 << 20 // 32 MB
const iconRequest = "/favicon.ico"
const iconFile = "./web/ico.png"
const errorHtml = "./web/error.html"
const keyForm = "image"
const linkHtml = "./web/link.html"

type errorPage struct {
	Number int
	Text   string
}

// структурами с данными
type Web struct {
	form       []byte
	imgDir     string
	db         *db.DB
	templLink  *template.Template
	templError *template.Template
}

func (h *Web) writeIcon(w http.ResponseWriter, r *http.Request) {
	// записать файл в буфер байт
	iconBuf, err := os.ReadFile(iconFile)

	if err != nil {
		http.Error(w, "cannot write icon file to buf: "+err.Error(), http.StatusBadRequest)
		log.Printf("cannot write icon file to buf: %v", err.Error())
		return
	}

	// записать иконку в клиентский сокет
	err = h.write(w, iconBuf, "cannot send icon file to the client", http.StatusInternalServerError)
	if err != nil {
		log.Printf("cannot send icon file to the client")
		return
	}

	log.Printf("The icon is written to the client's socket")
}

// метод, который записывает форму в сокет клиента
func (h *Web) Form(w http.ResponseWriter, r *http.Request) {
	log.Printf("PATH: %s", r.URL)

	// добавить иконку
	if r.URL.String() == iconRequest {
		// обработать запрос за иконкой
		h.writeIcon(w, r)
		return
	}

	// URL должен быть /
	if r.URL.String() != "/" {
		http.Redirect(w, r, "/", http.StatusFound)
		log.Printf("The request was redirected from address %q to address \"/\"", r.URL.String())
		return
	}

	// записать содержимое формы в сокет клиента
	if err := h.write(w, h.form, "cannot write form", http.StatusInternalServerError); err != nil {
		log.Printf("The form was not written to the client socket")
		return
	}
	log.Printf("The contents of the form are written to the client's socket")
}

// метод, который записывает картинку в сокет клиента
func (h *Web) ServeImage(w http.ResponseWriter, r *http.Request) {
	// извлечь из пути имя файла для поиска по бд
	key := path.Base(r.URL.String())

	log.Printf("A key for database search has been retrieved from the path: %s", key)

	// получить файл из бд
	file, err := h.db.Get(key)

	if err != nil {
		// файл не найден
		if db.RecordNotFound {
			if templErr := h.writeError(w, http.StatusText(http.StatusNotFound), http.StatusNotFound); templErr != nil {
				http.Error(w, "The template was not recorded: "+err.Error(), http.StatusInternalServerError)
				log.Printf("The template was not recorded")
			}
			return
		}
		http.Error(w, "cannot get file: "+err.Error(), http.StatusInternalServerError)
		log.Printf("cannot get file: %v", err.Error())
		return
	}

	log.Printf("A file was retrieved from the database: %s", key)

	// записать файл в клиентский сокет
	if err = h.write(w, file, "cannot write file", http.StatusInternalServerError); err != nil {
		log.Printf("The file was not written to the client socket")
		return
	}
	log.Printf("The contents of the %q have been sent to the client", key)
}

func (h *Web) write(w http.ResponseWriter, data []byte, errText string, errStatus int) error {
	bw, err := w.Write(data)
	if err != nil {
		if templErr := h.writeError(w, errText, errStatus); templErr != nil {
			http.Error(w, "The template was not recorded: "+err.Error(), http.StatusInternalServerError)
			log.Printf("The template was not recorded")
			return templErr
		}

		log.Printf("%s: %v", errText, err)
		return err
	}

	// размер файла должен совпадать с кол-вом записанных байт
	if bw != len(data) {
		log.Printf("%d byte expected, was received %d", len(data), bw)
		return fmt.Errorf("Partial write to the client")
	}

	return nil
}

func (h *Web) writeError(w http.ResponseWriter, errText string, errStatus int) error {
	w.WriteHeader(errStatus)
	// шаблон страницы с ошибкой
	errPage := &errorPage{
		Number: errStatus,
		Text:   http.StatusText(errStatus),
	}
	// запись шаблона с ошибкой в сокет клиента
	if err := h.templError.Execute(w, errPage); err != nil {
		return err
	}

	return nil
}

// функция-конструктор: создает экземпляр структуры с данными
func NewWeb(formName, imgDir string, db *db.DB) (*Web, error) {
	// прочитать содержимое файла с формой в срез байт
	f, err := os.ReadFile(formName)
	if err != nil {
		log.Printf("cannot read form file %s: %v", formName, err.Error())
		return nil, err
	}

	// получить шаблон файла с ссылкой:
	templLink, err := template.ParseFiles(linkHtml)
	if err != nil {
		log.Printf("cannot get template with link: %v", err.Error())
		return nil, err
	}

	// получить шаблон файла с ошибкой
	templError := template.Must(template.ParseFiles(errorHtml))

	// определение экземпляра структуры с данными для программы сервера
	serv := &Web{
		form:       f,
		imgDir:     imgDir,
		db:         db,
		templLink:  templLink,
		templError: templError,
	}

	// обработчики путей
	http.HandleFunc("/", serv.Form)
	http.HandleFunc("/upload", serv.Upload)
	http.HandleFunc("/images/", serv.ServeImage)

	return serv, nil
}

// метод, который запускает слушатель клиентских запросов на соединение
func (h *Web) Run(port string) error {
	server := &http.Server{Addr: "localhost:"+port}

	// подпрограмма с слушателем запросов
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server ListenAndServe: %v", err)
		}
	}()

	// захват сигналов
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// ожидание сигнала
	<-sigs
	log.Printf("The interrupt signal was intercepted")

	// контекст с таймером
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// корректное завершение работы в течение 10 секунд или закрытие
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("Server forced to shutdown: %v", err)
	}
	log.Printf("Server exiting gracefully")

	return nil
}

// метод, который обрабатывет POST запрос, сохраняет картинку в бд
func (h *Web) Upload(w http.ResponseWriter, r *http.Request) {
	// от клиента должен прийти POST запрос
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		log.Printf("Method %v not allowed", r.Method)
		return
	}
	log.Printf("Received a POST request from a client")

	// задает размер входящего буфера, больше которого из сети считывать не надо
	// ограничивает попытку вычитать серверу слишком большой запрос от клиента, который может уронить сервер
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	// метод, задает буфер для обработки maxMemory байт тела запроса в памяти, остальное временно хранит на диске
	// нужно, чтобы не исчерпать лимиты памяти
	err := r.ParseMultipartForm(maxUploadSize)
	if err != nil {
		http.Error(w, "cannot parse form: "+err.Error(), http.StatusBadRequest)
		log.Printf("cannot parse form: %v", err.Error())
		return
	}
	log.Printf("Processed no more than %d bytes of the request body", maxUploadSize)

	// func (r *Request) FormFile(key string) (multipart.File, *multipart.FileHeader, error)
	// file - интерфейс для доступа к файлу
	// fileheader - структура с полями:
	// type FileHeader struct {
	// Filename string
	// Header   textproto.MIMEHeader // заголовок в стиле MIME
	// Size     int64
	// }
	file, fileheader, err := r.FormFile(keyForm) // FormFile возвращает первый файл для ключа формы
	if err != nil {
		http.Error(w, "cannot get file from form: "+err.Error(), http.StatusBadRequest)
		log.Printf("cannot get file from form: %v", err.Error())
		return
	}

	// закрывает файл, делая его непригодным для ввода/вывода
	defer file.Close()

	fileBuf := bytes.NewBuffer(nil)

	// записать файл в байтовый срез
	_, err = io.Copy(fileBuf, file)

	if err != nil {
		http.Error(w, "cannot copy images to file on buf: "+err.Error(), http.StatusInternalServerError)
		log.Printf("cannot copy images to file on buf: %v", err.Error())
		return
	}

	log.Printf("The required file is written to the byte buffer")

	// сгенирировать ключ
	key, err := createKey(fileheader.Filename, h.db)
	if err != nil {
		http.Error(w, "cannot get a key to write to the database: "+err.Error(), http.StatusBadRequest)
		log.Printf("cannot get a key to write to the database: %v", err.Error())
		return
	}

	log.Printf("To store the file in the hash table, a key %q is generated", key)

	// записать в бд имя файла
	err = h.db.Set(fileheader.Filename, key, fileBuf.Bytes())

	if err != nil {
		http.Error(w, "The image has not been added to the database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("The file is written to the database")

	// сформировать ссылку для пользователя
	schemeDefault := "http://"
	scheme := schemeDefault
	if r.Header.Get("X-Forwarded-Proto") != "" {
		scheme = r.Header.Get("X-Forwarded-Proto") + "://"
	}
	addr := r.Header.Get("X-Forwarded-Host")
	dir := "/images"
	fileName := key
	userLink := scheme + addr + path.Join(dir, fileName)

	log.Printf("A link %q for the user has been generated", userLink)

	// шаблон получает данные для обработки и записывается в сокет клиента
	err = h.templLink.Execute(w, userLink)
	if err != nil {
		http.Error(w, "The template was not recorded: "+err.Error(), http.StatusInternalServerError)
		log.Printf("The template was not recorded")
		return
	}

	log.Printf("A link %q has been sent to the client", userLink)
}

const keyLen = 6
const startInt = 48
const endInt = 57
const startUpA = 65
const endUpA = 90
const startLoA = 97
const endLoA = 122

// создаю ключ
func createKey(str string, db *db.DB) (string, error) {
	var key *string

	for {
		// сгенерировать ключ
		key = generateKey()

		// проверить наличие ключа в бд
		isFound, err := db.IsExist(*key)

		if err != nil {
			return "", err
		}

		// сгенерированный ключ не должен совпадать с ключом какого-либо документа в бд
		if isFound == false {
			break
		}
	}

	// вернуть готовый ключ, по которому будет храниться имя файла
	return *key, nil
}

// генерирю ключ
func generateKey() *string {
	res := ""

	for lim := keyLen; lim > 0; lim-- {
		var c int

		// случайное числовое значение должно соответствовать букве английского алфавита или цифре
		for c < startInt || c > endInt && c < startUpA || c > endUpA && c < startLoA || c > endLoA {
			// сгенерировать случайное число
			c = rand.Intn(endLoA + 1)
		}

		res += string(byte(c))
	}

	return &res
}
