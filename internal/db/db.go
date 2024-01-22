package db

import (
	"context"
	"errors"
	"fmt"
	"log"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// структура с данными аутентификации
type Auth struct {
	AuthSource string
	Username   string
	Password   string
}

// структура данных объекта коллекции
type imageData struct {
	Filename string
	Key      string
	File     []byte
}

// бд с методами
type DB struct {
	db *mongo.Collection
}

// наличие документа в бд
var RecordNotFound bool

// добавить в бд
func (p *DB) Set(filename, key string, file []byte) error {

	// добавить картинку в коллекцию
	res, err := p.db.InsertOne(context.TODO(), &imageData{
		Filename: filename,
		Key:      key,
		File:     file,
	})

	if err != nil {
		log.Printf("The image %s has not been added to the database: %v", filename, err.Error())
		return err
	}

	log.Printf("A new document has been added to the database: %v", res.InsertedID)

	return nil
}

// проверить, записан ли уже в бд файл по ключу картинки
func (p *DB) IsExist(key string) (bool, error) {
	// bson.M — неупорядоченное представление документа BSON (порядок элементов не имеет значения)
	var resImage bson.M

	// найти картинку в бд и извлечь
	err := p.db.FindOne(context.TODO(), bson.D{{"key", key}}).Decode(&resImage) // bson.D - упорядоченное представление документа BSON
	// (порядок элементов имеет значения)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			log.Printf("The file was not found in the database: %v", err)
			return false, nil
		}
		log.Printf("The file was not extracted from the database: %v", err)
		return false, err
	}

	return true, nil
}

// получить файл по ключу картинки
func (p *DB) Get(key string) ([]byte, error) {
	var resImage bson.M

	// найти картинку в бд и извлечь
	err := p.db.FindOne(context.TODO(), bson.D{{"key", key}}).Decode(&resImage)

	// флаг отсутствия картинки в бд
	RecordNotFound = false

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			RecordNotFound = true
			log.Printf("The file was not found in the database: %v", err)
			return nil, err
		}
		log.Printf("The file was not extracted from the database: %v", err)
		return nil, fmt.Errorf("The file was not extracted from the database: %w", err)
	}

	m, err := bson.Marshal(resImage)

	if err != nil {
		log.Printf("BSON document not received: %v", err.Error())
		return nil, err
	}

	// bson.Raw(...) - полный документ bson
	// Lookup(...) - поиск в документе по ключу
	// Binary() - возвращает двоичное знач bson
	_, data := bson.Raw(m).Lookup("file").Binary()

	return data, nil
}

// функция-конструктор
func NewDB(URIDb, nameDb, nameCollection string, authData *Auth) (*DB, error) {
	collection, err := connectToDB(URIDb, nameDb, nameCollection, authData)
	if err != nil {
		return nil, err
	}
	return &DB{
		db: collection,
	}, nil
}

// создание подключения и инициализация коллекции бд
func connectToDB(URIDb, nameDb, nameCollection string, authData *Auth) (*mongo.Collection, error) {
	credential := options.Credential{
		AuthSource: authData.AuthSource,
		Username:   authData.Username,
		Password:   authData.Password,
	}

	// установить параметры клиента
	clientOptions := options.Client().ApplyURI(URIDb).SetAuth(credential)

	// подключиться к MongoDB
	client, err := mongo.Connect(context.TODO(), clientOptions)

	if err != nil {
		return nil, err
	}

	// проверить соединение
	err = client.Ping(context.TODO(), nil)

	if err != nil {
		return nil, err
	}

	// инициализировать обработчик коллекции
	imagesCollection := client.Database(nameDb).Collection(nameCollection)

	return imagesCollection, nil
}
