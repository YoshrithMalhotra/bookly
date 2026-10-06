import kotlin.js.Date

/** Date helpers. Times are always shown in the business's timezone, not the viewer's. */
object Fmt {
    private fun opts(vararg pairs: Pair<String, Any?>): dynamic {
        val o: dynamic = js("({})")
        for ((k, v) in pairs) o[k] = v
        return o
    }

    fun time(iso: String, tz: String): String =
        Date(iso).asDynamic().toLocaleTimeString(undefined, opts("hour" to "2-digit", "minute" to "2-digit", "timeZone" to tz)) as String

    fun dateLong(iso: String, tz: String): String =
        Date(iso).asDynamic().toLocaleDateString(
            undefined,
            opts("weekday" to "long", "day" to "numeric", "month" to "long", "timeZone" to tz),
        ) as String

    /** Heading for a YYYY-MM-DD date, e.g. "Monday 2 November". */
    fun day(date: String): String =
        Date(date + "T12:00:00Z").asDynamic().toLocaleDateString(
            undefined,
            opts("weekday" to "long", "day" to "numeric", "month" to "long", "timeZone" to "UTC"),
        ) as String

    /** Today's date (YYYY-MM-DD) in a timezone. */
    fun today(tz: String): String =
        Date().asDynamic().toLocaleDateString("en-CA", opts("timeZone" to tz)) as String

    fun addDays(date: String, n: Int): String {
        val d = Date(date + "T12:00:00Z")
        val shifted = Date(d.getTime() + n * 86_400_000.0)
        return (shifted.toISOString()).substring(0, 10)
    }

    /** 0 = Sunday, matching the API. */
    fun weekday(date: String): Int = Date(date + "T12:00:00Z").getUTCDay()

    fun browserTimezone(): String =
        js("Intl.DateTimeFormat().resolvedOptions().timeZone") as String? ?: "UTC"

    fun timezones(): List<String> {
        val list = js("typeof Intl.supportedValuesOf === 'function' ? Intl.supportedValuesOf('timeZone') : []")
            .unsafeCast<Array<String>>().toMutableList()
        val mine = browserTimezone()
        if (mine !in list) list.add(0, mine)
        return list
    }

    val weekdayNames = listOf("Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday")

    fun duration(min: Int): String = when {
        min < 60 -> "$min min"
        min % 60 == 0 -> "${min / 60} h"
        else -> "${min / 60} h ${min % 60} min"
    }
}
