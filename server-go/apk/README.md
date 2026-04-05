# APK Directory

Place the Android APK file here as `app-release.apk`.

This file is served at `/downloads/app-release.apk` by the server.

You can build the APK from the [vocdoni-passport](https://github.com/vocdoni/vocdoni-passport) repository:

```bash
make apk
cp out/app-release.apk /path/to/server-go/apk/
```
