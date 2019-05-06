package xwindow

import (
	"errors"
	"fmt"
	"image"
	"sync"

	"github.com/BurntSushi/xgb"
	mshm "github.com/BurntSushi/xgb/shm"
	"github.com/BurntSushi/xgb/xinerama"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/gen2brain/shm"
	"github.com/lukegb/screenshot/internal/util"
)

var (
	xgbConnOnce sync.Once
	xgbConn     *xgb.Conn
	xgbConnErr  error
)

func newXGBConn() (*xgb.Conn, error) {
	xgbConnOnce.Do(func() {
		xgbConn, xgbConnErr = xgb.NewConn()
	})
	return xgbConn, xgbConnErr
}

func Capture(x, y, width, height int) (img *image.RGBA, e error) {
	defer func() {
		err := recover()
		if err != nil {
			img = nil
			e = errors.New(fmt.Sprintf("%v", err))
		}
	}()
	c, err := newXGBConn()
	if err != nil {
		return nil, err
	}

	err = xinerama.Init(c)
	if err != nil {
		return nil, err
	}

	reply, err := xinerama.QueryScreens(c).Reply()
	if err != nil {
		return nil, err
	}

	primary := reply.ScreenInfo[0]
	x0 := int(primary.XOrg)
	y0 := int(primary.YOrg)

	useShm := true
	err = mshm.Init(c)
	if err != nil {
		useShm = false
	}

	screen := xproto.Setup(c).DefaultScreen(c)
	wholeScreenBounds := image.Rect(0, 0, int(screen.WidthInPixels), int(screen.HeightInPixels))
	targetBounds := image.Rect(x+x0, y+y0, x+x0+width, y+y0+height)
	intersect := wholeScreenBounds.Intersect(targetBounds)

	rect := image.Rect(0, 0, width, height)
	img, err = util.CreateImage(rect)
	if err != nil {
		return nil, err
	}

	// Paint with opaque black, based on the implementation of bytes.Repeat.
	bp := copy(img.Pix, []byte{0, 0, 0, 255})
	for bp < len(img.Pix) {
		copy(img.Pix[bp:], img.Pix[:bp])
		bp *= 2
	}

	if !intersect.Empty() {
		var data []byte

		if useShm {
			shmSize := intersect.Dx() * intersect.Dy() * 4
			shmId, err := shm.Get(shm.IPC_PRIVATE, shmSize, shm.IPC_CREAT|0777)
			if err != nil {
				return nil, err
			}

			seg, err := mshm.NewSegId(c)
			if err != nil {
				return nil, err
			}

			data, err = shm.At(shmId, 0, 0)
			if err != nil {
				return nil, err
			}

			mshm.Attach(c, seg, uint32(shmId), false)

			defer mshm.Detach(c, seg)
			defer shm.Rm(shmId)
			defer shm.Dt(data)

			_, err = mshm.GetImage(c, xproto.Drawable(screen.Root),
				int16(intersect.Min.X), int16(intersect.Min.Y),
				uint16(intersect.Dx()), uint16(intersect.Dy()), 0xffffffff,
				byte(xproto.ImageFormatZPixmap), seg, 0).Reply()
			if err != nil {
				return nil, err
			}
		} else {
			xImg, err := xproto.GetImage(c, xproto.ImageFormatZPixmap, xproto.Drawable(screen.Root),
				int16(intersect.Min.X), int16(intersect.Min.Y),
				uint16(intersect.Dx()), uint16(intersect.Dy()), 0xffffffff).Reply()
			if err != nil {
				return nil, err
			}

			data = xImg.Data
		}

		// BitBlt by hand
		offset := 0
		iRowOffset := (intersect.Min.Y - (y + y0)) * img.Stride
		iColOffset := (intersect.Min.X - (x + x0)) * 4
		io := iRowOffset + iColOffset
		stride := img.Stride - (intersect.Max.X-intersect.Min.X)*4
		for iy := intersect.Min.Y; iy < intersect.Max.Y; iy++ {
			for ix := intersect.Min.X; ix < intersect.Max.X; ix++ {
				px := img.Pix[io : io+4 : io+4]
				d := data[offset : offset+3 : offset+3]
				px[0] = d[2]
				px[1] = d[1]
				px[2] = d[0]
				px[3] = 255
				offset += 4
				io += 4
			}
			io += stride
		}
	}

	return img, e
}

func NumActiveDisplays() (num int) {
	defer func() {
		e := recover()
		if e != nil {
			num = 0
		}
	}()

	c, err := newXGBConn()
	if err != nil {
		return 0
	}

	err = xinerama.Init(c)
	if err != nil {
		return 0
	}

	reply, err := xinerama.QueryScreens(c).Reply()
	if err != nil {
		return 0
	}

	num = int(reply.Number)
	return num
}

func GetDisplayBounds(displayIndex int) (rect image.Rectangle) {
	defer func() {
		e := recover()
		if e != nil {
			rect = image.ZR
		}
	}()

	c, err := newXGBConn()
	if err != nil {
		return image.ZR
	}

	err = xinerama.Init(c)
	if err != nil {
		return image.ZR
	}

	reply, err := xinerama.QueryScreens(c).Reply()
	if err != nil {
		return image.ZR
	}

	if displayIndex >= int(reply.Number) {
		return image.ZR
	}

	primary := reply.ScreenInfo[0]
	x0 := int(primary.XOrg)
	y0 := int(primary.YOrg)

	screen := reply.ScreenInfo[displayIndex]
	x := int(screen.XOrg) - x0
	y := int(screen.YOrg) - y0
	w := int(screen.Width)
	h := int(screen.Height)
	rect = image.Rect(x, y, x+w, y+h)
	return rect
}
