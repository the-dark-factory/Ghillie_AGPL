--  gen_vectors — golden-vector generator for the ghillie v0 marshaller check.
--
--  SCRATCH TEST HARNESS, NOT A FACTORY COMPONENT, NOT SPARK (SPARK_Mode Off
--  below, deliberately). It is hand-written glue whose only job is to call the
--  PROVEN Encode of Facade_Instruction_Codec_Pkg (ledger 119, source_sha256
--  bb82366d...) over a fixed vector set and print the bytes it produces. It is
--  built outside the factory tree; the .ads it drives is a verbatim hash-checked
--  copy. Nothing here is proved and nothing here is meant to be.
--
--  Output format, one vector per line, '|'-separated:
--    name|command|protocol|artifact_ref|version|seq|wire_hex
--  Numeric fields are decimal, wire_hex is 44 lowercase hex chars (22 bytes).

pragma SPARK_Mode (Off);

with Ada.Text_IO;                  use Ada.Text_IO;
with Interfaces;                   use Interfaces;
with Facade_Instruction_Codec_Pkg; use Facade_Instruction_Codec_Pkg;

procedure Gen_Vectors is

   Hex_Digits : constant String := "0123456789abcdef";

   function To_Hex (W : Wire) return String is
      S : String (1 .. 44);
      J : Positive := 1;
   begin
      for I in W'Range loop
         S (J)     := Hex_Digits (Integer (Shift_Right (W (I), 4)) + 1);
         S (J + 1) := Hex_Digits (Integer (W (I) and 16#0F#) + 1);
         J := J + 2;
      end loop;
      return S;
   end To_Hex;

   function U8_Img (V : Unsigned_8) return String is
      S : constant String := Unsigned_8'Image (V);
   begin
      return S (S'First + 1 .. S'Last);
   end U8_Img;

   function U32_Img (V : Unsigned_32) return String is
      S : constant String := Unsigned_32'Image (V);
   begin
      return S (S'First + 1 .. S'Last);
   end U32_Img;

   function U64_Img (V : Unsigned_64) return String is
      S : constant String := Unsigned_64'Image (V);
   begin
      return S (S'First + 1 .. S'Last);
   end U64_Img;

   procedure Emit
     (Name         : String;
      Command      : Unsigned_8;
      Protocol     : Unsigned_8;
      Artifact_Ref : Unsigned_64;
      Version      : Unsigned_32;
      Seq          : Unsigned_64)
   is
      R : constant Instruction :=
        (Command      => Command,
         Protocol     => Protocol,
         Artifact_Ref => Artifact_Ref,
         Version      => Version,
         Seq          => Seq);
      W : constant Wire := Encode (R);
      D : constant Instruction := Decode (W);
   begin
      --  Belt and braces: the round-trip is a proved postcondition, but assert
      --  it at run time too so a mis-built harness cannot emit quiet nonsense.
      pragma Assert (D.Command = R.Command);
      pragma Assert (D.Protocol = R.Protocol);
      pragma Assert (D.Artifact_Ref = R.Artifact_Ref);
      pragma Assert (D.Version = R.Version);
      pragma Assert (D.Seq = R.Seq);

      Put_Line
        (Name & "|" & U8_Img (Command) & "|" & U8_Img (Protocol) & "|"
         & U64_Img (Artifact_Ref) & "|" & U32_Img (Version) & "|"
         & U64_Img (Seq) & "|" & To_Hex (W));
   end Emit;

   U64_Max : constant Unsigned_64 := Unsigned_64'Last;
   U32_Max : constant Unsigned_32 := Unsigned_32'Last;
   U8_Max  : constant Unsigned_8  := Unsigned_8'Last;

begin
   --  zero and all-ones
   Emit ("zero",     0,      0,      0,       0,       0);
   Emit ("all_ones", U8_Max, U8_Max, U64_Max, U32_Max, U64_Max);

   --  each field isolated: max, most-significant bit, least-significant bit
   Emit ("command_max",   U8_Max, 0, 0, 0, 0);
   Emit ("command_msb",   16#80#, 0, 0, 0, 0);
   Emit ("command_lsb",   1,      0, 0, 0, 0);

   Emit ("protocol_max",  0, U8_Max, 0, 0, 0);
   Emit ("protocol_msb",  0, 16#80#, 0, 0, 0);
   Emit ("protocol_lsb",  0, 1,      0, 0, 0);

   Emit ("artifact_ref_max",     0, 0, U64_Max,                 0, 0);
   Emit ("artifact_ref_msb",     0, 0, 16#8000_0000_0000_0000#, 0, 0);
   Emit ("artifact_ref_lsb",     0, 0, 1,                       0, 0);
   Emit ("artifact_ref_pattern", 0, 0, 16#0123_4567_89AB_CDEF#, 0, 0);
   Emit ("artifact_ref_2p32",    0, 0, 16#0000_0001_0000_0000#, 0, 0);

   Emit ("version_max",     0, 0, 0, U32_Max,       0);
   Emit ("version_msb",     0, 0, 0, 16#8000_0000#, 0);
   Emit ("version_lsb",     0, 0, 0, 1,             0);
   Emit ("version_pattern", 0, 0, 0, 16#0123_4567#, 0);
   Emit ("version_2p16",    0, 0, 0, 16#0001_0000#, 0);

   Emit ("seq_max",     0, 0, 0, 0, U64_Max);
   Emit ("seq_msb",     0, 0, 0, 0, 16#8000_0000_0000_0000#);
   Emit ("seq_lsb",     0, 0, 0, 0, 1);
   Emit ("seq_pattern", 0, 0, 0, 0, 16#FEDC_BA98_7654_3210#);
   Emit ("seq_2p56",    0, 0, 0, 0, 16#0100_0000_0000_0000#);

   --  every command rank in the proven enumeration, protocol version 1
   Emit ("cmd_report_status",       0, 1, 0,     0, 1);
   Emit ("cmd_offer_catalogue",     1, 1, 4_097, 1, 2);
   Emit ("cmd_deliver_artifact",    2, 1, 16#DEAD_BEEF_CAFE_F00D#, 7, 3);
   Emit ("cmd_request_spec_upload", 3, 1, 0,     0, 4);
   Emit ("cmd_install_artifact",    4, 1, 16#0000_0000_0000_00FF#, 16#FFFF_FFFE#, 5);
   Emit ("cmd_run_local_code",      5, 1, 1,     1, 16#7FFF_FFFF_FFFF_FFFF#);

   --  mixed boundary crossings
   Emit ("mixed_alternating", 16#AA#, 16#55#,
         16#AAAA_AAAA_AAAA_AAAA#, 16#5555_5555#, 16#AAAA_5555_AAAA_5555#);
   Emit ("mixed_near_max",    16#FE#, 16#01#,
         U64_Max - 1, U32_Max - 1, U64_Max - 1);
end Gen_Vectors;
