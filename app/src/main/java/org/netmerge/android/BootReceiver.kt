package org.netmerge.android

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.net.VpnService

/** Starts merging when the phone starts, if the setting is on and the VPN permission is still there. */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED) return
        if (!Engine.wantsStartOnBoot(context) || VpnService.prepare(context) != null) return
        context.startForegroundService(
            Intent(context, MergeVpnService::class.java).setAction(MergeVpnService.ACTION_BOOT),
        )
    }
}
