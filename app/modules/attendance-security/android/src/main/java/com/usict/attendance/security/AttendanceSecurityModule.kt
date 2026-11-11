package com.usict.attendance.security

import android.util.Base64
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import expo.modules.kotlin.records.Field
import expo.modules.kotlin.records.Record

class AttendanceFieldsRecord : Record {
    @Field val challengeId: String = ""
    @Field val nonce: String = ""
    @Field val facultyId: String = ""
    @Field val latMicro: Double = 0.0
    @Field val lonMicro: Double = 0.0
    @Field val accuracyCm: Double = 0.0
}

class AttendanceV2FieldsRecord : Record {
    @Field val action: String = ""
    @Field val challengeId: String = ""
    @Field val nonce: String = ""
    @Field val facultyId: String = ""
    @Field val latMicro: Double = 0.0
    @Field val lonMicro: Double = 0.0
    @Field val accuracyCm: Double = 0.0
}

class RequestOutFieldsRecord : Record {
    @Field val action: String = "request_out"
    @Field val challengeId: String = ""
    @Field val nonce: String = ""
    @Field val facultyId: String = ""
    @Field val latMicro: Double = 0.0
    @Field val lonMicro: Double = 0.0
    @Field val accuracyCm: Double = 0.0
}

class HistoryFieldsRecord : Record {
    @Field val challengeId: String = ""
    @Field val nonce: String = ""
    @Field val facultyId: String = ""
}

class AttendanceSecurityModule : Module() {
    override fun definition() = ModuleDefinition {
        Name("AttendanceSecurity")

        AsyncFunction("hasKey") { KeyManager.hasKey() }

        AsyncFunction("getKeyInfo") {
            if (!KeyManager.hasKey()) null else mapOf("securityLevel" to KeyManager.securityLevel())
        }

        AsyncFunction("generateKey") {
            KeyManager.generate()
            mapOf(
                "publicKey" to Base64.encodeToString(KeyManager.publicKeyDer(), Base64.NO_WRAP),
                "securityLevel" to KeyManager.securityLevel(),
            )
        }

        // JS passes structured fields only; this module builds the signed string itself, so a
        // compromised JS layer cannot use the key to sign anything else.
        AsyncFunction("signAttendance") { f: AttendanceFieldsRecord ->
            val payload = PayloadBuilder.attend(
                f.challengeId, f.nonce, f.facultyId,
                f.latMicro.toLong(), f.lonMicro.toLong(), f.accuracyCm.toLong(),
            )
            mapOf("signature" to Signer.sign(payload.toByteArray(Charsets.UTF_8)))
        }

        // v2 (IN/OUT). PayloadBuilder.attendV2 rejects any action other than "in"/"out".
        AsyncFunction("signAttendanceV2") { f: AttendanceV2FieldsRecord ->
            val payload = PayloadBuilder.attendV2(
                f.action, f.challengeId, f.nonce, f.facultyId,
                f.latMicro.toLong(), f.lonMicro.toLong(), f.accuracyCm.toLong(),
            )
            mapOf("signature" to Signer.sign(payload.toByteArray(Charsets.UTF_8)))
        }

        AsyncFunction("signRequestOut") { f: RequestOutFieldsRecord ->
            val payload = PayloadBuilder.requestOut(
                f.action, f.challengeId, f.nonce, f.facultyId,
                f.latMicro.toLong(), f.lonMicro.toLong(), f.accuracyCm.toLong(),
            )
            mapOf("signature" to Signer.sign(payload.toByteArray(Charsets.UTF_8)))
        }

        AsyncFunction("signHistory") { f: HistoryFieldsRecord ->
            val payload = PayloadBuilder.history(f.challengeId, f.nonce, f.facultyId)
            mapOf("signature" to Signer.sign(payload.toByteArray(Charsets.UTF_8)))
        }

        AsyncFunction("deleteKey") { KeyManager.delete() }
    }
}
