package main

import (
	"fmt"
	"net/http"
	"os"
    //"time"
    "context"
    //"errors"
	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"google.golang.org/appengine"
    "path"
    "io"
    "google.golang.org/appengine/datastore"
    "google.golang.org/appengine/image"
    "google.golang.org/appengine/blobstore"
    "encoding/json"
     "google.golang.org/appengine/log"
)

var (
    photoBucketName string
    photoBucket     *storage.BucketHandle
)

type Photo struct {
	Name          string
    PublicURL     string
    ServingURL    string
	Votes         int
}

type Voter struct {
    Murmur        string
    Votes         int
}

func main() {
    photoBucketName = os.Getenv("UPLOADABLE_BUCKET")
    ctx := context.Background()
    client,_ := storage.NewClient(ctx)
	photoBucket = client.Bucket(photoBucketName)

	http.HandleFunc("/uploadPhoto", uploadPhotoHandler)
    http.HandleFunc("/listPhoto", listPhotoHandler)
    http.HandleFunc("/votePhoto", votePhotoHandler)
    http.HandleFunc("/getVoter",getVoterHandler)
    http.HandleFunc("/addVotes",addVotesHandler)
    appengine.Main()
}
    
func uploadPhoto(r *http.Request) (url string, err error) {
	f, fh, err := r.FormFile("image")

	// random filename, retaining existing extension.
	name := uuid.Must(uuid.New(),err).String() + path.Ext(fh.Filename,)

	ctx := context.Background()
	w := photoBucket.Object(name).NewWriter(ctx)

	// Warning: storage.AllUsers gives public read access to anyone.
	w.ACL = []storage.ACLRule{{Entity: storage.AllUsers, Role: storage.RoleReader}}
	w.ContentType = fh.Header.Get("Content-Type")

	// Entries are immutable, be aggressive about caching (1 day).
	w.CacheControl = "public, max-age=86400"
	//const publicURL = 

	if _, err := io.Copy(w, f); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	return name, nil
}

func storeDB(r *http.Request,name string) (err error) {
    ctx := appengine.NewContext(r)

    // struct to be stored
    photo := new(Photo)

    // filename from photo
    photo.Name = name

    // public URL from the bucket
    photo.PublicURL = fmt.Sprintf("https://storage.googleapis.com/%s/%s",photoBucketName,name)

    // get the legacy blobstore key
    blobFilename := fmt.Sprintf("/gs/%s/%s",photoBucketName,name)
    blobkey,_ := blobstore.BlobKeyForFile(ctx,blobFilename)

    // options for the serving URL ( for automatic scaling )
    var servingURLOptions image.ServingURLOptions
    servingURLOptions.Secure = true
    servingURLOptions.Size = 1200
    servingURLOptions.Crop = false

    // get the serving URL
    servingURL,_ := image.ServingURL(ctx, blobkey,&servingURLOptions)
    photo.ServingURL = servingURL.String()

    // all photos start with one vote
    photo.Votes = 0

    // create a key with the name as the ID and store it
    key := datastore.NewKey(ctx, "Photo", name, 0, nil)
    if _, err := datastore.Put(ctx, key, photo); err != nil {
        return err
    }
    return nil
}

func uploadPhotoHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Origin", "*")
	publicURL,berr := uploadPhoto(r)
    derr := storeDB(r,publicURL)
    fmt.Fprintf(w,"URL %s\nBucketErr %s\nDatastoreErr %s\n",publicURL,berr,derr)
    fmt.Fprintf(w, "Done.\n")
}

func addVotesHandler(w http.ResponseWriter, r *http.Request) {
    ctx := appengine.NewContext(r)
    voters := make([]*Voter, 0)
    q := datastore.NewQuery("Voter").Filter("Votes<",5)

    keys, _ := q.GetAll(ctx, &voters)

    log.Debugf(ctx, "Found [%d] voters", len(keys))

    for _,voter := range voters {
        voter.Votes = voter.Votes+1
    }

    if _, err := datastore.PutMulti(ctx, keys, voters); err != nil {
        w.WriteHeader(http.StatusInternalServerError)
        log.Debugf(ctx, "Error increasing votes:\n%s\n", err)
        return
    }

    fmt.Fprintf(w,"Added votes to [%d] keys\n",len(keys))
}

func getVoterHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Origin", "*")

    r.ParseForm()

    ctx := appengine.NewContext(r)
    voter := new(Voter)
    key := datastore.NewKey(ctx, "Voter", r.FormValue("voter"), 0, nil)

    if err := datastore.Get(ctx, key, voter); err != nil {
        voter.Murmur = r.FormValue("voter")
        voter.Votes = 5
        datastore.Put(ctx,key,voter)
        log.Debugf(ctx, "New voter [%s]\n", voter.Murmur)
    }

    log.Debugf(ctx, "Found voter [%s]\n", voter.Murmur)

    jsonVoter, _ := json.Marshal(voter)
    w.Header().Set("Content-Type", "application/json")
    w.Write(jsonVoter)
}

func votePhotoHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Origin", "*")

    r.ParseForm()

    ctx := appengine.NewContext(r)

    voter := new(Voter)
    voterKey := datastore.NewKey(ctx, "Voter", r.FormValue("voter"), 0, nil)

    if err := datastore.Get(ctx, voterKey, voter); err != nil {
        w.WriteHeader(http.StatusInternalServerError)
        log.Debugf(ctx, "Error getting voter key:\n%s\n", err)
        return
    }

    photo := new(Photo)
    photoKey := datastore.NewKey(ctx, "Photo", r.FormValue("name"), 0, nil)

    if err := datastore.Get(ctx, photoKey, photo); err != nil {
        w.WriteHeader(http.StatusInternalServerError)
        log.Debugf(ctx, "Error getting photo key:\n%s\n", err)
        return
    }

    if voter.Votes > 0 {
        voter.Votes = voter.Votes - 1
        photo.Votes = photo.Votes + 1
    }

    if _, err := datastore.Put(ctx, voterKey, voter); err != nil {
        w.WriteHeader(http.StatusInternalServerError)
        log.Debugf(ctx, "Error saving voter:\n%s\n", err)
        return
    }

    if _, err := datastore.Put(ctx, photoKey, photo); err != nil {
        w.WriteHeader(http.StatusInternalServerError)
        log.Debugf(ctx, "Error saving vote:\n%s\n", err)
        return
    }

    log.Debugf(ctx, "Voter [%s] voted for photo [%s] ", voter.Murmur, photo.Name)

    w.Header().Set("Content-Type", "application/json")
    //w.Write(jsonPhotos)
}

func listPhotoHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Origin", "*")

    ctx := appengine.NewContext(r)
    photos := make([]*Photo, 0)
    q := datastore.NewQuery("Photo").
            Order("-Votes")

    keys, _ := q.GetAll(ctx, &photos)

    log.Debugf(ctx, "Found [%d] keys", len(keys))

    jsonPhotos, _ := json.Marshal(photos)
    w.Header().Set("Content-Type", "application/json")
    w.Write(jsonPhotos)
}