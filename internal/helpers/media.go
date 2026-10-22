package helpers

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/chromedp/chromedp"
)

// chromeSem limits concurrent headless Chrome instances
var chromeSem = make(chan struct{}, 4)

const chromeRenderTimeout = 90 * time.Second
const chromeAcquireTimeout = 10 * time.Second

func acquireChromeSlot() error {
	timer := time.NewTimer(chromeAcquireTimeout)
	defer timer.Stop()
	select {
	case chromeSem <- struct{}{}:
		return nil
	case <-timer.C:
		return fmt.Errorf("timed out waiting for a media renderer")
	}
}

func discardChromedpLogf(string, ...interface{}) {}

type PDFPage struct {
	URL    string
	Height float64
	Width  float64
}

func pdfGrabber(ctx context.Context, pdf *PDFPage) ([]byte, error) {
	if err := chromedp.Do(ctx, chromedp.Navigate(pdf.URL), chromedp.WaitVisible(chromedp.CSS("body"))); err != nil {
		return nil, err
	}
	return chromedp.Run(ctx, chromedp.PrintToPDF(
		chromedp.PDFPrintBackground(),
		chromedp.PDFPreferCSSPageSize(),
		chromedp.PDFPaper(chromedp.PaperSize{Width: pdf.Width, Height: pdf.Height}),
	))
}

func BuildChromePdf(ctx *config.AppContext, pdfPage *PDFPage) ([]byte, error) {
	if err := acquireChromeSlot(); err != nil {
		return nil, err
	}
	defer func() { <-chromeSem }()

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserAgent("WebScraper 1.0"),
		chromedp.Flag("allow-insecure-localhost", true),
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("accept-insecure-certs", true),
	)

	baseCtx, timeoutCancel := context.WithTimeout(context.Background(), chromeRenderTimeout)
	defer timeoutCancel()

	allocCtx, cancel := chromedp.NewExecAllocator(baseCtx, opts...)
	defer cancel()

	taskCtx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	pdfBuffer, err := pdfGrabber(taskCtx, pdfPage)
	if err != nil {
		ctx.Err.Printf("error loading URL: %s", pdfPage.URL)
		return pdfBuffer, err
	}

	return pdfBuffer, nil
}

func pngGrabber(ctx context.Context, pg *PDFPage) ([]byte, error) {
	// Convert inches to pixels at 96 DPI.
	if err := chromedp.Do(ctx,
		chromedp.EmulateViewport(int64(pg.Width*96), int64(pg.Height*96)),
		chromedp.Navigate(pg.URL),
		chromedp.WaitVisible(chromedp.CSS("body")),
	); err != nil {
		return nil, err
	}
	return chromedp.Run(ctx, chromedp.FullScreenshot(100))
}

func BuildChromePng(ctx *config.AppContext, pdfPage *PDFPage) ([]byte, error) {
	if err := acquireChromeSlot(); err != nil {
		return nil, err
	}
	defer func() { <-chromeSem }()

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserAgent("WebScraper 1.0"),
		chromedp.Flag("allow-insecure-localhost", true),
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("accept-insecure-certs", true),
	)

	baseCtx, timeoutCancel := context.WithTimeout(context.Background(), chromeRenderTimeout)
	defer timeoutCancel()

	allocCtx, cancel := chromedp.NewExecAllocator(baseCtx, opts...)
	defer cancel()

	taskCtx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	pngBuffer, err := pngGrabber(taskCtx, pdfPage)
	if err != nil {
		ctx.Err.Printf("error taking screenshot: %s", pdfPage.URL)
		return pngBuffer, err
	}

	return pngBuffer, nil
}

type MediaRenderer struct {
	ctx           *config.AppContext
	allocCtx      context.Context
	cancelAlloc   context.CancelFunc
	browserCtx    context.Context
	cancelBrowser context.CancelFunc
	mu            sync.Mutex
}

func NewMediaRenderer(ctx *config.AppContext) (*MediaRenderer, error) {
	if err := acquireChromeSlot(); err != nil {
		return nil, err
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserAgent("WebScraper 1.0"),
		chromedp.Flag("allow-insecure-localhost", true),
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("accept-insecure-certs", true),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	browserCtx, cancelBrowser := chromedp.NewContext(
		allocCtx,
		chromedp.WithLogf(discardChromedpLogf),
		chromedp.WithErrorf(discardChromedpLogf),
	)

	return &MediaRenderer{
		ctx:           ctx,
		allocCtx:      allocCtx,
		cancelAlloc:   cancelAlloc,
		browserCtx:    browserCtx,
		cancelBrowser: cancelBrowser,
	}, nil
}

func (r *MediaRenderer) Close() {
	if r == nil {
		return
	}
	r.cancelBrowser()
	r.cancelAlloc()
	<-chromeSem
}

func (r *MediaRenderer) BuildChromePng(pdfPage *PDFPage) ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("nil media renderer")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	tabCtx, cancelTab := chromedp.NewContext(r.browserCtx)
	defer cancelTab()
	taskCtx, cancelTimeout := context.WithTimeout(tabCtx, chromeRenderTimeout)
	defer cancelTimeout()

	pngBuffer, err := pngGrabber(taskCtx, pdfPage)
	if err != nil {
		r.ctx.Err.Printf("error taking screenshot: %s", pdfPage.URL)
		return pngBuffer, err
	}

	return pngBuffer, nil
}

func (r *MediaRenderer) MakeMediaPng(card, path string) ([]byte, error) {
	dimens, ok := types.MediaDimens[card]
	if !ok {
		return nil, fmt.Errorf("can't find card %s", card)
	}

	pg := &PDFPage{
		URL:    r.ctx.Env.GetURI() + signedMediaPath(r.ctx, path),
		Height: dimens.Height,
		Width:  dimens.Width,
	}

	r.ctx.Infos.Printf("PNG URL: %s", pg.URL)
	return r.BuildChromePng(pg)
}

func (r *MediaRenderer) MakeSpeakerPng(confTag, card, speakerID, talkID string) ([]byte, error) {
	path := fmt.Sprintf("/media/imgs/%s/speaker/%s/%s/%s", confTag, card, talkID, speakerID)
	return r.MakeMediaPng(card, path)
}

func (r *MediaRenderer) MakeTalkPng(confTag, card, talkID string) ([]byte, error) {
	path := fmt.Sprintf("/media/imgs/%s/talk/%s/%s", confTag, card, talkID)
	return r.MakeMediaPng(card, path)
}

func (r *MediaRenderer) MakeSponsorPng(confTag, card, sponsorRef string) ([]byte, error) {
	path := fmt.Sprintf("/media/imgs/%s/sponsor/%s/%s", confTag, card, sponsorRef)
	return r.MakeMediaPng(card, path)
}

func MakeMediaPng(ctx *config.AppContext, card, path string) ([]byte, error) {
	dimens, ok := types.MediaDimens[card]
	if !ok {
		return nil, fmt.Errorf("can't find card %s", card)
	}

	pg := &PDFPage{
		URL:    ctx.Env.GetURI() + signedMediaPath(ctx, path),
		Height: dimens.Height,
		Width:  dimens.Width,
	}

	ctx.Infos.Printf("PNG URL: %s", pg.URL)
	return BuildChromePng(ctx, pg)
}

func signedMediaPath(ctx *config.AppContext, path string) string {
	q := url.Values{}
	q.Set("mt", CreateScopedHMAC(ctx, "media-render", path))
	return path + "?" + q.Encode()
}

func MakeSpeakerPng(ctx *config.AppContext, confTag, card, speakerID, talkID string) ([]byte, error) {
	path := fmt.Sprintf("/media/imgs/%s/speaker/%s/%s/%s", confTag, card, talkID, speakerID)
	return MakeMediaPng(ctx, card, path)
}

func MakeTalkPng(ctx *config.AppContext, confTag, card, talkID string) ([]byte, error) {
	path := fmt.Sprintf("/media/imgs/%s/talk/%s/%s", confTag, card, talkID)
	return MakeMediaPng(ctx, card, path)
}

func MakeSponsorPng(ctx *config.AppContext, confTag, card, sponsorRef string) ([]byte, error) {
	path := fmt.Sprintf("/media/imgs/%s/sponsor/%s/%s", confTag, card, sponsorRef)
	return MakeMediaPng(ctx, card, path)
}

func MakeAgendaImg(ctx *config.AppContext, confTag, dayref, venue string) ([]byte, error) {
	path := fmt.Sprintf("/media/imgs/%s/agenda/%s/%s", confTag, dayref, venue)
	return MakeMediaPng(ctx, "agenda", path)
}
