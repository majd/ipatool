package unicorn

import "fmt"

const unicornVersion = "2.1.4"

type artifact struct {
	url           string
	archiveSHA256 string
	librarySHA256 string
	member        string
	filename      string
	format        archiveFormat
	dependencies  []artifact
}

func artifactFor(goos, goarch string) (artifact, error) {
	switch goos + "/" + goarch {
	case "darwin/amd64":
		return artifact{
			url:           "https://files.pythonhosted.org/packages/c8/a7/92b47771e2107a201632a199cec91e8a81ee8a071ca6b7e7d600d8c61ac9/unicorn-2.1.4-cp37-abi3-macosx_10_9_x86_64.whl",
			archiveSHA256: "2a6f738fab5fabffa56af1e7bbf16ea1e91466c342f8dc64f125bd70f36c6b80",
			librarySHA256: "51c4a6f3ce22628ecd3acd1c49b921a818ffb989ca2c473134cc7eb06094f256",
			member:        "unicorn/lib/libunicorn.2.dylib",
			filename:      "libunicorn.2.dylib",
		}, nil
	case "darwin/arm64":
		return artifact{
			url:           "https://files.pythonhosted.org/packages/6c/ae/4943c6f8524d729ec7d5e69df6407ea05d710fe77471d91cecf3fc64eb57/unicorn-2.1.4-cp37-abi3-macosx_11_0_arm64.whl",
			archiveSHA256: "d6c93e0f60328d8f4a1792af3f834137a28050fcc2305f2ec01efe8558a9844e",
			librarySHA256: "7207c8e3d7a63118fb0bca73e01816797fd51b1d8a39a4cbc7abfd562ee59c85",
			member:        "unicorn/lib/libunicorn.2.dylib",
			filename:      "libunicorn.2.dylib",
		}, nil
	case "linux/amd64":
		return artifact{
			url:           "https://files.pythonhosted.org/packages/e7/df/ded5e3684c2d7600b30cc8a7530277b8cb36644a1a9d34cade7ebb45604c/unicorn-2.1.4-cp37-abi3-manylinux_2_17_x86_64.manylinux2014_x86_64.whl",
			archiveSHA256: "9d6e6dea140560de4ebd8446661f7ef84a357d428c14a3ef09dacd306ec8c239",
			librarySHA256: "ddb196ec82b52e502c18e4a34478bf7b9f61c83c2ebaa95c74d8ded45a95da9c",
			member:        "unicorn/lib/libunicorn.so.2",
			filename:      "libunicorn.so.2",
		}, nil
	case "linux/arm64":
		return artifact{
			url:           "https://files.pythonhosted.org/packages/33/9f/32d41eb942221bcf4417cdc65537fc8b3bbbd6079d6c161e621f1dd4e94a/unicorn-2.1.4-cp37-abi3-manylinux_2_17_aarch64.manylinux2014_aarch64.whl",
			archiveSHA256: "bd1fb0c9af5f57e356d8a96928b4fe045b2e18f308ef23b481d5f970008aa722",
			librarySHA256: "a0b99458a82e268aee258205a40590411c3a9f28e42abf2942ce4e87b7d9ac65",
			member:        "unicorn/lib/libunicorn.so.2",
			filename:      "libunicorn.so.2",
		}, nil
	case "linux-musl/amd64":
		return artifact{
			url:           "https://files.pythonhosted.org/packages/ed/4b/4628ccb20eb3ad1af400de8181d1f4e5c1a3fc2affa1b3410c1b2d71af36/unicorn-2.1.4-cp37-abi3-musllinux_1_2_x86_64.whl",
			archiveSHA256: "d348a90ee90219d141cb115ef8ed7e3fd1af42afaee105f7580761d775b25e32",
			librarySHA256: "cc1a208c69b151fdd23439736b0fac9ac6e14409dae77deee900369e6daab302",
			member:        "unicorn/lib/libunicorn.so.2",
			filename:      "libunicorn.so.2",
		}, nil
	case "linux-musl/arm64":
		return artifact{
			url:           "https://files.pythonhosted.org/packages/70/38/ba5a051c844026e59ab6e0017db8cec77dbe20ab5f1d6edae1ce9d885b06/unicorn-2.1.4-cp37-abi3-musllinux_1_2_aarch64.whl",
			archiveSHA256: "01d744ba01c5cc68f1d7afe3d183f1868720fd440ec4eaedc4d1d5d9bf54b84c",
			librarySHA256: "52179305928b32c937d2d527ad6fef9d500c6fa7cdb14bf32abf7021d67271a2",
			member:        "unicorn/lib/libunicorn.so.2",
			filename:      "libunicorn.so.2",
		}, nil
	case "windows/amd64":
		return artifact{
			url:           "https://github.com/unicorn-engine/unicorn/releases/download/2.1.4/windows-mingw64-shared.7z",
			archiveSHA256: "0960f938e66fa12c448742bddd2a03aa88abeeb2b3cda7156493a2da86228d3a",
			librarySHA256: "d8f9a89222ffa74493a1d47090e17f8e1db8ac171a3128c6a76a4ea09de11469",
			member:        "bin/libunicorn.dll",
			filename:      "libunicorn.dll",
			format:        archiveSevenZip,
		}, nil
	case "windows/arm64":
		return artifact{
			url:           "https://mirror.msys2.org/mingw/clangarm64/mingw-w64-clang-aarch64-unicorn-2.1.4-5-any.pkg.tar.zst",
			archiveSHA256: "e28aab2165d9cff048c29c58d6a40eb97928b23cf8ddeb78056a4b5b9805ac61",
			librarySHA256: "0ee1ebab91653645ef2b0615a8225123f7af9a49df4b6fcc5fb5d45d540ae9c2",
			member:        "clangarm64/bin/libunicorn.dll",
			filename:      "libunicorn.dll",
			format:        archiveTarZstd,
			dependencies: []artifact{
				{
					url:           "https://mirror.msys2.org/mingw/clangarm64/mingw-w64-clang-aarch64-libwinpthread-14.0.0.r302.gd7f3c5201-1-any.pkg.tar.zst",
					archiveSHA256: "dd20ad17543608915a2ff9ef6f39146d5621298531e0f50706fd1e78bf1da834",
					librarySHA256: "b80722a2586c0d1de605724569a564f3c139d184deaa33b7df7415477d733467",
					member:        "clangarm64/bin/libwinpthread-1.dll",
					filename:      "libwinpthread-1.dll",
					format:        archiveTarZstd,
				},
			},
		}, nil
	default:
		return artifact{}, fmt.Errorf("unsupported platform %s/%s", goos, goarch)
	}
}
