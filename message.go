package whatsapp

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/Rhymen/go-whatsapp/binary"
	"github.com/Rhymen/go-whatsapp/binary/proto"
)

type messageType string

const (
	image    messageType = "WhatsApp Image Keys"
	video    messageType = "WhatsApp Video Keys"
	audio    messageType = "WhatsApp Audio Keys"
	document messageType = "WhatsApp Document Keys"
	location messageType = "WhatsApp Location Keys"
)

func (wac *Conn) Send(msg interface{}) error {
	var err error
	var ch <-chan string

	switch m := msg.(type) {
	case TextMessage:
		ch, err = wac.sendProto(getTextProto(m))
	case ImageMessage:
		m.url, m.mediaKey, m.fileEncSha256, m.fileSha256, m.fileLength, err = wac.upload(m.Content, image)
		if err != nil {
			return fmt.Errorf("upload failed: %v", err)
		}
		ch, err = wac.sendProto(getImageProto(m))
	case VideoMessage:
		m.url, m.mediaKey, m.fileEncSha256, m.fileSha256, m.fileLength, err = wac.upload(m.Content, video)
		if err != nil {
			return fmt.Errorf("upload failed: %v", err)
		}
		ch, err = wac.sendProto(getVideoProto(m))
	case DocumentMessage:
		m.url, m.mediaKey, m.fileEncSha256, m.fileSha256, m.fileLength, err = wac.upload(m.Content, document)
		if err != nil {
			return fmt.Errorf("upload failed: %v", err)
		}
		ch, err = wac.sendProto(getDocumentProto(m))
	case AudioMessage:
		m.url, m.mediaKey, m.fileEncSha256, m.fileSha256, m.fileLength, err = wac.upload(m.Content, audio)
		if err != nil {
			return fmt.Errorf("upload failed: %v", err)
		}
		ch, err = wac.sendProto(getAudioProto(m))
	default:
		return fmt.Errorf("cannot match type %T, use messagetypes declared in the package", msg)
	}
	if err != nil {
		return fmt.Errorf("error sending message:%v", err)
	}

	select {
	case response := <-ch:
		var resp map[string]interface{}
		if err = json.Unmarshal([]byte(response), &resp); err != nil {
			return fmt.Errorf("error decoding sending response: %v\n", err)
		}
		if int(resp["status"].(float64)) != 200 {
			return fmt.Errorf("message sending responded with %d", resp["status"])
		}
	case <-time.After(wac.msgTimeout):
		return fmt.Errorf("sending message timed out")
	}

	return nil
}

func (wac *Conn) sendProto(p *proto.WebMessageInfo) (<-chan string, error) {
	n := binary.Node{
		Description: "action",
		Attributes: map[string]string{
			"type":  "relay",
			"epoch": strconv.Itoa(wac.msgCount),
		},
		Content: []interface{}{p},
	}
	return wac.writeBinary(n, message, ignore, p.Key.GetId())
}

func init() {
	rand.Seed(time.Now().UTC().UnixNano())
}

/*
MessageInfo contains general message information. It is part of every of every message type.
*/
type MessageInfo struct {
	Id        string
	RemoteJid string
	FromMe    bool
	Timestamp uint64
	PushName  string
}

func getMessageInfo(msg *proto.WebMessageInfo) MessageInfo {
	return MessageInfo{
		Id:        msg.GetKey().GetId(),
		RemoteJid: msg.GetKey().GetRemoteJid(),
		FromMe:    msg.GetKey().GetFromMe(),
		Timestamp: msg.GetMessageTimestamp(),
		PushName:  msg.GetPushName(),
	}
}

func getInfoProto(info *MessageInfo) *proto.WebMessageInfo {
	status := proto.WebMessageInfo_STATUS(1)
	if info.Id == "" || len(info.Id) < 2 {
		b := make([]byte, 10)
		rand.Read(b)
		info.Id = strings.ToUpper(hex.EncodeToString(b))
	}
	if info.Timestamp == 0 {
		info.Timestamp = uint64(time.Now().Unix())
	}
	info.FromMe = true

	return &proto.WebMessageInfo{
		Key: &proto.MessageKey{
			FromMe:    &info.FromMe,
			RemoteJid: &info.RemoteJid,
			Id:        &info.Id,
		},
		MessageTimestamp: &info.Timestamp,
		Status:           &status,
	}
}

/*
LocationMessage represents a location sharing message.
*/
type LocationMessage struct {
	Info             MessageInfo
	DegreesLatitude  float64
	DegreesLongitude float64
	Url              string
	Address          string
}

func getLocationMessage(msg *proto.WebMessageInfo) LocationMessage {
	return LocationMessage{
		Info:             getMessageInfo(msg),
		DegreesLatitude:  msg.GetMessage().LocationMessage.GetDegreesLatitude(),
		DegreesLongitude: msg.GetMessage().LocationMessage.GetDegreesLongitude(),
		Url:              msg.GetMessage().LocationMessage.GetUrl(),
		Address:          msg.GetMessage().LocationMessage.GetAddress(),
	}
}

func getLocationProto(msg LocationMessage) *proto.WebMessageInfo {
	p := getInfoProto(&msg.Info)
	p.Message = &proto.Message{
		LocationMessage: &proto.LocationMessage{
			DegreesLatitude:  &msg.DegreesLatitude,
			DegreesLongitude: &msg.DegreesLongitude,
			Url:              &msg.Url,
			Address:          &msg.Address,
		},
	}
	return p
}

/*
TextMessage represents a text message.
*/
type TextMessage struct {
	Info MessageInfo
	Text string
}

func getTextMessage(msg *proto.WebMessageInfo) TextMessage {
	return TextMessage{
		Info: getMessageInfo(msg),
		Text: msg.GetMessage().GetConversation(),
	}
}

func getTextProto(msg TextMessage) *proto.WebMessageInfo {
	p := getInfoProto(&msg.Info)
	p.Message = &proto.Message{
		Conversation: &msg.Text,
	}
	return p
}

/*
ImageMessage represents a image message. Unexported fields are needed for media up/downloading and media validation.
Provide a io.Reader as Content for message sending.
*/
type ImageMessage struct {
	Info          MessageInfo
	Caption       string
	Thumbnail     []byte
	Type          string
	Content       io.Reader
	url           string
	mediaKey      []byte
	fileEncSha256 []byte
	fileSha256    []byte
	fileLength    uint64
}

func getImageMessage(msg *proto.WebMessageInfo) ImageMessage {
	image := msg.GetMessage().GetImageMessage()
	return ImageMessage{
		Info:          getMessageInfo(msg),
		Caption:       image.GetCaption(),
		Thumbnail:     image.GetJpegThumbnail(),
		url:           image.GetUrl(),
		mediaKey:      image.GetMediaKey(),
		Type:          image.GetMimetype(),
		fileEncSha256: image.GetFileEncSha256(),
		fileSha256:    image.GetFileSha256(),
		fileLength:    image.GetFileLength(),
	}
}

func getImageProto(msg ImageMessage) *proto.WebMessageInfo {
	p := getInfoProto(&msg.Info)
	p.Message = &proto.Message{
		ImageMessage: &proto.ImageMessage{
			Caption:       &msg.Caption,
			JpegThumbnail: msg.Thumbnail,
			Url:           &msg.url,
			MediaKey:      msg.mediaKey,
			Mimetype:      &msg.Type,
			FileEncSha256: msg.fileEncSha256,
			FileSha256:    msg.fileSha256,
			FileLength:    &msg.fileLength,
		},
	}
	return p
}

/*
Download is the function to retrieve media data. The media gets downloaded, validated and returned.
*/
func (m *ImageMessage) Download() ([]byte, error) {
	return download(m.url, m.mediaKey, image, int(m.fileLength))
}

/*
VideoMessage represents a video message. Unexported fields are needed for media up/downloading and media validation.
Provide a io.Reader as Content for message sending.
*/
type VideoMessage struct {
	Info          MessageInfo
	Caption       string
	Thumbnail     []byte
	Length        uint32
	Type          string
	Content       io.Reader
	url           string
	mediaKey      []byte
	fileEncSha256 []byte
	fileSha256    []byte
	fileLength    uint64
}

func getVideoMessage(msg *proto.WebMessageInfo) VideoMessage {
	vid := msg.GetMessage().GetVideoMessage()
	return VideoMessage{
		Info:          getMessageInfo(msg),
		Caption:       vid.GetCaption(),
		Thumbnail:     vid.GetJpegThumbnail(),
		url:           vid.GetUrl(),
		mediaKey:      vid.GetMediaKey(),
		Length:        vid.GetSeconds(),
		Type:          vid.GetMimetype(),
		fileEncSha256: vid.GetFileEncSha256(),
		fileSha256:    vid.GetFileSha256(),
		fileLength:    vid.GetFileLength(),
	}
}

func getVideoProto(msg VideoMessage) *proto.WebMessageInfo {
	p := getInfoProto(&msg.Info)
	p.Message = &proto.Message{
		VideoMessage: &proto.VideoMessage{
			Caption:       &msg.Caption,
			JpegThumbnail: msg.Thumbnail,
			Url:           &msg.url,
			MediaKey:      msg.mediaKey,
			Seconds:       &msg.Length,
			FileEncSha256: msg.fileEncSha256,
			FileSha256:    msg.fileSha256,
			FileLength:    &msg.fileLength,
			Mimetype:      &msg.Type,
		},
	}
	return p
}

/*
Download is the function to retrieve media data. The media gets downloaded, validated and returned.
*/
func (m *VideoMessage) Download() ([]byte, error) {
	return download(m.url, m.mediaKey, video, int(m.fileLength))
}

/*
AudioMessage represents a audio message. Unexported fields are needed for media up/downloading and media validation.
Provide a io.Reader as Content for message sending.
*/
type AudioMessage struct {
	Info          MessageInfo
	Length        uint32
	Type          string
	Content       io.Reader
	url           string
	mediaKey      []byte
	fileEncSha256 []byte
	fileSha256    []byte
	fileLength    uint64
}

func getAudioMessage(msg *proto.WebMessageInfo) AudioMessage {
	aud := msg.GetMessage().GetAudioMessage()
	return AudioMessage{
		Info:          getMessageInfo(msg),
		url:           aud.GetUrl(),
		mediaKey:      aud.GetMediaKey(),
		Length:        aud.GetSeconds(),
		Type:          aud.GetMimetype(),
		fileEncSha256: aud.GetFileEncSha256(),
		fileSha256:    aud.GetFileSha256(),
		fileLength:    aud.GetFileLength(),
	}
}

func getAudioProto(msg AudioMessage) *proto.WebMessageInfo {
	p := getInfoProto(&msg.Info)
	p.Message = &proto.Message{
		AudioMessage: &proto.AudioMessage{
			Url:           &msg.url,
			MediaKey:      msg.mediaKey,
			Seconds:       &msg.Length,
			FileEncSha256: msg.fileEncSha256,
			FileSha256:    msg.fileSha256,
			FileLength:    &msg.fileLength,
			Mimetype:      &msg.Type,
		},
	}
	return p
}

/*
Download is the function to retrieve media data. The media gets downloaded, validated and returned.
*/
func (m *AudioMessage) Download() ([]byte, error) {
	return download(m.url, m.mediaKey, audio, int(m.fileLength))
}

/*
DocumentMessage represents a document message. Unexported fields are needed for media up/downloading and media
validation. Provide a io.Reader as Content for message sending.
*/
type DocumentMessage struct {
	Info          MessageInfo
	Title         string
	PageCount     uint32
	Type          string
	Thumbnail     []byte
	Content       io.Reader
	url           string
	mediaKey      []byte
	fileEncSha256 []byte
	fileSha256    []byte
	fileLength    uint64
}

func getDocumentMessage(msg *proto.WebMessageInfo) DocumentMessage {
	doc := msg.GetMessage().GetDocumentMessage()
	return DocumentMessage{
		Info:          getMessageInfo(msg),
		Thumbnail:     doc.GetJpegThumbnail(),
		url:           doc.GetUrl(),
		mediaKey:      doc.GetMediaKey(),
		fileEncSha256: doc.GetFileEncSha256(),
		fileSha256:    doc.GetFileSha256(),
		fileLength:    doc.GetFileLength(),
		PageCount:     doc.GetPageCount(),
		Title:         doc.GetTitle(),
		Type:          doc.GetMimetype(),
	}
}

func getDocumentProto(msg DocumentMessage) *proto.WebMessageInfo {
	p := getInfoProto(&msg.Info)
	p.Message = &proto.Message{
		DocumentMessage: &proto.DocumentMessage{
			JpegThumbnail: msg.Thumbnail,
			Url:           &msg.url,
			MediaKey:      msg.mediaKey,
			FileEncSha256: msg.fileEncSha256,
			FileSha256:    msg.fileSha256,
			FileLength:    &msg.fileLength,
			PageCount:     &msg.PageCount,
			Title:         &msg.Title,
			Mimetype:      &msg.Type,
		},
	}
	return p
}

/*
Download is the function to retrieve media data. The media gets downloaded, validated and returned.
*/
func (m *DocumentMessage) Download() ([]byte, error) {
	return download(m.url, m.mediaKey, document, int(m.fileLength))
}

func parseProtoMessage(msg *proto.WebMessageInfo) interface{} {
	switch {

	case msg.GetMessage().GetAudioMessage() != nil:
		return getAudioMessage(msg)

	case msg.GetMessage().GetImageMessage() != nil:
		return getImageMessage(msg)

	case msg.GetMessage().GetVideoMessage() != nil:
		return getVideoMessage(msg)

	case msg.GetMessage().GetDocumentMessage() != nil:
		return getDocumentMessage(msg)

	case msg.GetMessage().GetConversation() != "":
		return getTextMessage(msg)

	case msg.GetMessage().GetLocationMessage() != nil:
		return getLocationMessage(msg)

	default:
		//cannot match message
	}

	return nil
}
