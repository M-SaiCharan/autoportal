// Command genassets writes the build-time assets that can't live in plain
// Go code: the Windows resource files (icon, manifest, version info)
// linked into autoportal.exe, and the macOS app icon.
//
//	go run ./tools/genassets -version v0.2.0
package main

import (
	"flag"
	"fmt"
	"image"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/josephspurrier/goversioninfo"

	"github.com/M-SaiCharan/autoportal/internal/icon"
)

// The manifest asks for modern-looking dialogs (common controls v6) and
// sharp text on high-DPI screens, and declares that no admin is needed.
const manifest = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity type="win32" name="io.github.m-saicharan.autoportal" version="%s"/>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0"
        processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security><requestedPrivileges><requestedExecutionLevel level="asInvoker" uiAccess="false"/></requestedPrivileges></security>
  </trustInfo>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application><supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/></application>
  </compatibility>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2</dpiAwareness>
    </windowsSettings>
  </application>
</assembly>
`

func main() {
	version := flag.String("version", "dev", "release version, e.g. v0.2.0")
	out := flag.String("out", "dist/assets", "folder for icon files")
	syso := flag.String("syso", "cmd/autoportal", "folder for the Windows .syso resource files")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	write := func(name string, data []byte) string {
		p := filepath.Join(*out, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			log.Fatal(err)
		}
		return p
	}
	ico := write("autoportal.ico", icon.AppICO())
	write("autoportal.icns", icon.ICNS(func(s int) *image.NRGBA { return icon.App(s) }))
	write("autoportal.png", icon.PNG(icon.App(512)))

	nums := versionNumbers(*version)
	dotted := fmt.Sprintf("%d.%d.%d.0", nums[0], nums[1], nums[2])
	man := write("autoportal.manifest", []byte(fmt.Sprintf(manifest, dotted)))

	fv := goversioninfo.FileVersion{Major: nums[0], Minor: nums[1], Patch: nums[2]}
	vi := &goversioninfo.VersionInfo{
		IconPath:     ico,
		ManifestPath: man,
	}
	vi.FixedFileInfo.FileVersion = fv
	vi.FixedFileInfo.ProductVersion = fv
	vi.FixedFileInfo.FileFlagsMask = "3f"
	vi.FixedFileInfo.FileOS = "040004"
	vi.FixedFileInfo.FileType = "01"
	vi.StringFileInfo = goversioninfo.StringFileInfo{
		CompanyName:      "Sai Charan",
		FileDescription:  "autoportal: automatic campus Wi-Fi login",
		FileVersion:      dotted,
		InternalName:     "autoportal",
		LegalCopyright:   "Copyright (c) Sai Charan. MIT License.",
		OriginalFilename: "autoportal.exe",
		ProductName:      "autoportal",
		ProductVersion:   strings.TrimPrefix(*version, "v"),
	}
	vi.VarFileInfo.Translation = goversioninfo.Translation{LangID: goversioninfo.LngUSEnglish, CharsetID: goversioninfo.CsUnicode}
	vi.Build()
	vi.Walk()
	for _, arch := range []string{"amd64", "arm64"} {
		p := filepath.Join(*syso, "resource_windows_"+arch+".syso")
		if err := vi.WriteSyso(p, arch); err != nil {
			log.Fatalf("%s: %v", p, err)
		}
	}
	fmt.Println("wrote icons to", *out, "and Windows resources to", *syso)
}

// versionNumbers turns "v1.2.3" (or "v1.2.3-4-gabc") into {1, 2, 3}; anything
// else becomes {0, 0, 0}.
func versionNumbers(v string) [3]int {
	var n [3]int
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return n
	}
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil {
			return [3]int{}
		}
		n[i] = x
	}
	return n
}
