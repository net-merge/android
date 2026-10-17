package io.github.senseiredsmoke.netmerge

import android.app.PendingIntent
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService

/** Quick Settings toggle. Opens the app when VPN permission hasn't been granted yet. */
class MergeTileService : TileService() {

    override fun onStartListening() = refresh()

    override fun onClick() {
        if (MergeVpnService.running) {
            MergeVpnService.stop(this)
        } else if (VpnService.prepare(this) == null) {
            MergeVpnService.start(this)
        } else {
            val intent = Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
                startActivityAndCollapse(PendingIntent.getActivity(this, 0, intent, PendingIntent.FLAG_IMMUTABLE))
            } else {
                @Suppress("DEPRECATION")
                startActivityAndCollapse(intent)
            }
            return
        }
        // The service flips `running` asynchronously; show the intended state right away.
        val tile = qsTile ?: return
        tile.state = if (tile.state == Tile.STATE_ACTIVE) Tile.STATE_INACTIVE else Tile.STATE_ACTIVE
        tile.updateTile()
    }

    private fun refresh() {
        val tile = qsTile ?: return
        tile.state = if (MergeVpnService.running) Tile.STATE_ACTIVE else Tile.STATE_INACTIVE
        tile.updateTile()
    }
}
