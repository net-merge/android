package io.github.senseiredsmoke.netmerge

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.VpnService
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.ParcelFileDescriptor
import android.service.quicksettings.TileService
import android.util.Log
import android.content.ComponentName
import org.json.JSONArray
import org.json.JSONObject

/**
 * Captures all traffic with a TUN and hands it to the Go engine, which re-dials every flow over
 * Wi-Fi or mobile data. The app itself is excluded from the VPN, so the engine's own sockets
 * (pinned to a specific network) leave the phone directly.
 */
class MergeVpnService : VpnService() {

    companion object {
        const val ACTION_START = "io.github.senseiredsmoke.netmerge.START"
        const val ACTION_STOP = "io.github.senseiredsmoke.netmerge.STOP"
        private const val TAG = "NetMerge"
        private const val CHANNEL = "status"
        private const val NOTIF_ID = 1
        private const val MTU = 9000

        @Volatile
        var running = false
            private set

        fun start(ctx: Context) {
            ctx.startForegroundService(Intent(ctx, MergeVpnService::class.java).setAction(ACTION_START))
        }

        fun stop(ctx: Context) {
            ctx.startService(Intent(ctx, MergeVpnService::class.java).setAction(ACTION_STOP))
        }
    }

    private var tun: ParcelFileDescriptor? = null
    private var tracker: NetworkTracker? = null
    private val main = Handler(Looper.getMainLooper())
    private val refresh = object : Runnable {
        override fun run() {
            updateNotification()
            main.postDelayed(this, 2000)
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            shutdown()
            return START_NOT_STICKY
        }
        // ACTION_START, or the system starting us for always-on VPN (action android.net.VpnService).
        goForeground()
        if (!running) startVpn()
        return START_STICKY
    }

    private fun startVpn() {
        val t = NetworkTracker(this) { nets -> main.post { onNetworks(nets) } }
        tracker = t
        t.start()

        val builder = Builder()
            .setSession(getString(R.string.app_name))
            .setMtu(MTU)
            .addAddress("198.18.0.1", 24)
            .addRoute("0.0.0.0", 0)
            // DNS goes into the TUN like everything else and is balanced over both networks.
            .addDnsServer("1.1.1.1")
            .addDnsServer("8.8.8.8")
            .addDisallowedApplication(packageName)
            .setConfigureIntent(
                PendingIntent.getActivity(
                    this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE,
                ),
            )
        // IPv6: a ULA so apps still prefer IPv4 on dual-stack sites (see upstream tun.go).
        runCatching {
            builder.addAddress("fd6e:6d65:7267::1", 64)
            builder.addRoute("::", 0)
            builder.addDnsServer("2606:4700:4700::1111")
        }.onFailure { Log.w(TAG, "IPv6 not captured", it) }

        val pfd = try {
            builder.establish()
        } catch (e: Exception) {
            Log.e(TAG, "establish failed", e)
            null
        }
        if (pfd == null) {
            // Not prepared (permission revoked) or another VPN took over.
            shutdown()
            return
        }
        tun = pfd
        onNetworks(t.current())
        try {
            // The engine owns the descriptor from here and closes it in stop().
            netmerge.Netmerge.start(pfd.detachFd().toLong(), MTU.toLong())
        } catch (e: Exception) {
            Log.e(TAG, "engine start failed", e)
            shutdown()
            return
        }
        running = true
        main.post(refresh)
        requestTileUpdate()
    }

    private fun onNetworks(nets: List<NetworkTracker.Tracked>) {
        val arr = JSONArray()
        for (n in nets) {
            arr.put(
                JSONObject()
                    .put("name", n.name)
                    .put("handle", n.network.networkHandle.toString())
                    .put("v4", n.v4)
                    .put("v6", n.v6),
            )
        }
        runCatching { netmerge.Netmerge.setNetworks(arr.toString()) }
            .onFailure { Log.e(TAG, "setNetworks", it) }
        // Tells the system which networks we really use (metering, status bar icons).
        setUnderlyingNetworks(nets.map { it.network }.toTypedArray())
        updateNotification()
    }

    private fun shutdown() {
        main.removeCallbacks(refresh)
        if (running) netmerge.Netmerge.stop()
        running = false
        tracker?.stop()
        tracker = null
        runCatching { tun?.close() }
        tun = null
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
        requestTileUpdate()
    }

    override fun onRevoke() {
        // Another VPN started or the user turned us off in Settings.
        shutdown()
    }

    override fun onDestroy() {
        if (running) shutdown()
        super.onDestroy()
    }

    private fun requestTileUpdate() {
        TileService.requestListeningState(this, ComponentName(this, MergeTileService::class.java))
    }

    // --- notification ---

    private fun goForeground() {
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(
            NotificationChannel(CHANNEL, getString(R.string.channel_name), NotificationManager.IMPORTANCE_LOW),
        )
        val n = buildNotification(getString(R.string.notif_waiting))
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            // systemExempted covers active VPN apps; fall back to specialUse if the system disagrees.
            try {
                startForeground(NOTIF_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_SYSTEM_EXEMPTED)
            } catch (e: SecurityException) {
                startForeground(NOTIF_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
            }
        } else {
            startForeground(NOTIF_ID, n)
        }
    }

    private fun buildNotification(text: String): Notification {
        val open = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE,
        )
        val stop = PendingIntent.getService(
            this, 1, Intent(this, MergeVpnService::class.java).setAction(ACTION_STOP), PendingIntent.FLAG_IMMUTABLE,
        )
        return Notification.Builder(this, CHANNEL)
            .setSmallIcon(R.drawable.ic_merge)
            .setContentTitle(getString(R.string.notif_title))
            .setContentText(text)
            .setStyle(Notification.BigTextStyle().bigText(text))
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setContentIntent(open)
            .addAction(Notification.Action.Builder(null, getString(R.string.stop), stop).build())
            .build()
    }

    private fun updateNotification() {
        if (!running) return
        val stats = Stats.parse(netmerge.Netmerge.stats()).filter { it.present }
        val text = if (stats.isEmpty()) getString(R.string.notif_waiting)
        else stats.joinToString("\n") { "${it.name}: ${Stats.rate(it.rateIn + it.rateOut)} · ${it.status}" }
        getSystemService(NotificationManager::class.java).notify(NOTIF_ID, buildNotification(text))
    }
}
