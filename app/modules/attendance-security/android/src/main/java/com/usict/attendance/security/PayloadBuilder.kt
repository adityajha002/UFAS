package com.usict.attendance.security

/**
 * Pure Kotlin (no Android imports) so a JVM unit test can assert the golden vectors.
 *
 * These MUST match the server's crypto.go byte for byte:
 *   attend.v1|challengeID|nonce|facultyID|latMicro|lonMicro|accuracyCm          (attendMsg)
 *   attend.v2|action|challengeID|nonce|facultyID|latMicro|lonMicro|accuracyCm   (attendMsgV2)
 *   history.v1|challengeID|nonce|facultyID                                      (historyMsg)
 */
object PayloadBuilder {
    /** The only actions v2 may sign. Checked here so JS cannot sign any other text. */
    val ACTIONS = setOf("in", "out")
    val REQUEST_ACTIONS = setOf("request_in", "request_out")
    const val REQUEST_IN = "request_in"
    const val REQUEST_OUT = "request_out"

    fun attend(
        challengeId: String, nonce: String, facultyId: String,
        latMicro: Long, lonMicro: Long, accuracyCm: Long,
    ): String = "attend.v1|$challengeId|$nonce|$facultyId|$latMicro|$lonMicro|$accuracyCm"

    /** @throws IllegalArgumentException("bad_action") unless [action] is "in" or "out". */
    fun attendV2(
        action: String, challengeId: String, nonce: String, facultyId: String,
        latMicro: Long, lonMicro: Long, accuracyCm: Long,
    ): String {
        require(action in ACTIONS) { "bad_action" }
        return "attend.v2|$action|$challengeId|$nonce|$facultyId|$latMicro|$lonMicro|$accuracyCm"
    }

    fun requestOut(
        action: String = REQUEST_OUT,
        challengeId: String, nonce: String, facultyId: String,
        latMicro: Long, lonMicro: Long, accuracyCm: Long,
    ): String {
        require(action in REQUEST_ACTIONS) { "bad_action" }
        return "attend.v2|$action|$challengeId|$nonce|$facultyId|$latMicro|$lonMicro|$accuracyCm"
    }

    fun history(challengeId: String, nonce: String, facultyId: String): String =
        "history.v1|$challengeId|$nonce|$facultyId"
}
