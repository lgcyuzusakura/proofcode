package dev.proofcode.control.provider;

import java.math.BigDecimal;
import java.util.List;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class ModelSettingsTest {
    private ModelSettings.PriceTier tier(int upper) { return new ModelSettings.PriceTier(upper,new BigDecimal("2"),new BigDecimal("8"),null,null); }
    private ModelSettings settings(List<ModelSettings.PriceTier> prices) { return new ModelSettings(1000000,900000,32000,"aggressive","CNY",prices); }
    @Test void acceptsCustomLimitsAndTierBoundaries() { assertEquals(3,settings(List.of(tier(10000),tier(24000),tier(0))).validated().prices().size()); }
    @Test void missingLimitsAndPricesUseOriginalDefaults() {
        assertEquals(ModelSettings.defaults(),new ModelSettings(null,null,null,null,null,null).validated());
    }
    @Test void rejectsUnorderedAndUnboundedMiddleTiers() {
        assertThrows(IllegalArgumentException.class,()->settings(List.of(tier(24000),tier(10000))).validated());
        assertThrows(IllegalArgumentException.class,()->settings(List.of(tier(0),tier(24000))).validated());
        assertThrows(IllegalArgumentException.class,()->new ModelSettings(1024,900,200,"balanced","CNY",List.of()).validated());
        assertThrows(IllegalArgumentException.class,()->settings(List.of(new ModelSettings.PriceTier(0,BigDecimal.valueOf(-1),BigDecimal.ZERO,null,null))).validated());
    }
}
