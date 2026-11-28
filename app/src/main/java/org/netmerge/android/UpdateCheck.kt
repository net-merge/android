package org.netmerge.android

import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Asks GitHub for the newest release at start and every 6 hours, like the PC version. It only tells
 * the user: installing stays their choice. The app is outside its own VPN, so this leaves directly.
 */
object UpdateCheck {
    const val RELEASES = "https://github.com/net-merge/android/releases/latest"
    private const val API = "https://api.github.com/repos/net-merge/android/releases/latest"
    private const val EVERY_MS = 6 * 60 * 60 * 1000L
    private val started = AtomicBoolean(false)

    fun start() {
        if (!started.compareAndSet(false, true)) return
        Thread({
            while (true) {
                runCatching {
                    val tag = latest()
                    if (netmerge.Netmerge.newer(BuildConfig.VERSION_NAME, tag)) Engine.setUpdate(tag)
                }
                Thread.sleep(EVERY_MS)
            }
        }, "update-check").apply { isDaemon = true }.start()
    }

    private fun latest(): String {
        val c = URL(API).openConnection() as HttpURLConnection
        c.connectTimeout = 8000
        c.readTimeout = 8000
        c.setRequestProperty("Accept", "application/vnd.github+json")
        c.setRequestProperty("User-Agent", "net-merge-android")
        try {
            // A release carries its notes, so the reply can be tens of kB; the limit only guards
            // against a wrong server answering.
            val body = c.inputStream.use { String(it.readNBytesCompat(1 shl 20)) }
            return JSONObject(body).getString("tag_name")
        } finally {
            c.disconnect()
        }
    }

    private fun java.io.InputStream.readNBytesCompat(max: Int): ByteArray {
        val out = java.io.ByteArrayOutputStream()
        val buf = ByteArray(8192)
        while (out.size() < max) {
            val n = read(buf, 0, minOf(buf.size, max - out.size()))
            if (n < 0) break
            out.write(buf, 0, n)
        }
        return out.toByteArray()
    }
}
