package io.github.senseiredsmoke.netmerge

import org.json.JSONArray

/** Parsed form of the engine's per-network stats (netmerge.Netmerge.stats()). */
data class Stats(
    val name: String,
    val state: String,
    val state6: String,
    val active: Long,
    val total: Long,
    val failed: Long,
    val bytesIn: Long,
    val bytesOut: Long,
    val rateIn: Double,
    val rateOut: Double,
    val weight: Double,
    val present: Boolean,
) {
    /** "up", "checking" or "no internet", folding IPv4 and IPv6 into one word. */
    val status: String
        get() = when {
            state == "up" || state6 == "up" -> "up"
            state == "checking" || state6 == "checking" -> "checking"
            else -> "no internet"
        }

    companion object {
        fun parse(json: String): List<Stats> {
            val arr = runCatching { JSONArray(json) }.getOrNull() ?: return emptyList()
            return (0 until arr.length()).map { i ->
                val o = arr.getJSONObject(i)
                Stats(
                    o.optString("name"), o.optString("state"), o.optString("state6"),
                    o.optLong("active"), o.optLong("total"), o.optLong("failed"),
                    o.optLong("bytesIn"), o.optLong("bytesOut"),
                    o.optDouble("rateIn", 0.0), o.optDouble("rateOut", 0.0),
                    o.optDouble("weight", 1.0), o.optBoolean("present"),
                )
            }
        }

        fun rate(bytesPerSec: Double): String = size(bytesPerSec) + "/s"

        fun size(bytes: Double): String = when {
            bytes >= 1e9 -> "%.1f GB".format(bytes / 1e9)
            bytes >= 1e6 -> "%.1f MB".format(bytes / 1e6)
            bytes >= 1e3 -> "%.0f kB".format(bytes / 1e3)
            else -> "%.0f B".format(bytes)
        }
    }
}
