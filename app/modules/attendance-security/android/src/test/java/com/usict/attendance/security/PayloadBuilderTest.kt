package com.usict.attendance.security

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class PayloadBuilderTest {
    // Golden vectors: must equal the Go side for the same inputs.
    //   attendMsg("c1","n1","f1",28600000,77200000,1500)
    //   historyMsg("c1","n1","f1")
    @Test fun attendMatchesGoldenVector() {
        assertEquals(
            "attend.v1|c1|n1|f1|28600000|77200000|1500",
            PayloadBuilder.attend("c1", "n1", "f1", 28600000, 77200000, 1500),
        )
    }

    // 00_OVERVIEW.md §5.1 golden vector; the server's attendMsgV2 (SRV-2) asserts the same.
    @Test fun attendV2MatchesGoldenVector() {
        assertEquals(
            "attend.v2|out|c0ffee|bm9uY2U|EMP001|28606827|77209000|1500",
            PayloadBuilder.attendV2("out", "c0ffee", "bm9uY2U", "EMP001", 28606827, 77209000, 1500),
        )
    }

    @Test fun attendV2In() {
        assertEquals(
            "attend.v2|in|c0ffee|bm9uY2U|EMP001|-1|-2|0",
            PayloadBuilder.attendV2("in", "c0ffee", "bm9uY2U", "EMP001", -1, -2, 0),
        )
    }

    @Test fun attendV2RejectsOtherActions() {
        for (bad in listOf("", "IN", "Out", "in|x", "attend")) {
            assertThrows(IllegalArgumentException::class.java) {
                PayloadBuilder.attendV2(bad, "c", "n", "f", 1, 2, 3)
            }
        }
    }

    @Test fun historyMatchesGoldenVector() {
        assertEquals("history.v1|c1|n1|f1", PayloadBuilder.history("c1", "n1", "f1"))
    }
}
