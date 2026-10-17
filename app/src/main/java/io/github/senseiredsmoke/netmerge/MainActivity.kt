package io.github.senseiredsmoke.netmerge

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Typeface
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.util.TypedValue
import android.view.ViewGroup.LayoutParams.MATCH_PARENT
import android.view.ViewGroup.LayoutParams.WRAP_CONTENT
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.Switch
import android.widget.TextView
import android.widget.Toast

/** One switch plus a live per-network view. */
class MainActivity : Activity() {

    private companion object {
        const val REQ_VPN = 1
        const val REQ_NOTIF = 2
    }

    private lateinit var toggle: Switch
    private lateinit var list: TextView
    private val main = Handler(Looper.getMainLooper())
    private val tick = object : Runnable {
        override fun run() {
            render()
            main.postDelayed(this, 1000)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val pad = dp(20)
        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(pad, pad, pad, pad)
            fitsSystemWindows = true
        }
        toggle = Switch(this).apply {
            text = getString(R.string.switch_label)
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 18f)
            setOnClickListener { onToggle(isChecked) }
        }
        list = TextView(this).apply {
            typeface = Typeface.MONOSPACE
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 14f)
            setPadding(0, dp(24), 0, dp(24))
        }
        val hint = TextView(this).apply {
            text = getString(R.string.hint)
            alpha = 0.75f
        }
        val credit = TextView(this).apply {
            text = getString(R.string.credit)
            alpha = 0.5f
            setPadding(0, dp(16), 0, 0)
        }
        root.addView(toggle, LinearLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT))
        root.addView(list)
        root.addView(hint)
        root.addView(credit)
        setContentView(ScrollView(this).apply { addView(root) })

        if (Build.VERSION.SDK_INT >= 33 &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), REQ_NOTIF)
        }
    }

    override fun onResume() {
        super.onResume()
        main.post(tick)
    }

    override fun onPause() {
        main.removeCallbacks(tick)
        super.onPause()
    }

    private fun onToggle(on: Boolean) {
        if (!on) {
            MergeVpnService.stop(this)
            return
        }
        val consent = VpnService.prepare(this)
        if (consent != null) {
            @Suppress("DEPRECATION")
            startActivityForResult(consent, REQ_VPN)
        } else {
            MergeVpnService.start(this)
        }
    }

    @Deprecated("Deprecated in Java")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        @Suppress("DEPRECATION")
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != REQ_VPN) return
        if (resultCode == RESULT_OK) {
            MergeVpnService.start(this)
        } else {
            toggle.isChecked = false
            Toast.makeText(this, R.string.vpn_denied, Toast.LENGTH_LONG).show()
        }
    }

    private fun render() {
        val running = MergeVpnService.running
        if (!toggle.isPressed) toggle.isChecked = running
        if (!running) {
            list.text = ""
            return
        }
        val stats = Stats.parse(netmerge.Netmerge.stats()).filter { it.present }
        if (stats.isEmpty()) {
            list.text = getString(R.string.no_networks)
            return
        }
        val total = stats.sumOf { it.rateIn + it.rateOut }
        list.text = buildString {
            for (s in stats) {
                append(s.name).append("  (").append(s.status).append(")\n")
                append("  ↓ ").append(Stats.rate(s.rateIn)).append("   ↑ ").append(Stats.rate(s.rateOut)).append('\n')
                append("  ").append(s.active).append(" open, ").append(s.total).append(" total, ")
                append(Stats.size((s.bytesIn + s.bytesOut).toDouble())).append(" moved\n\n")
            }
            append("Combined: ").append(Stats.rate(total))
        }
    }

    private fun dp(v: Int) = (v * resources.displayMetrics.density).toInt()
}
